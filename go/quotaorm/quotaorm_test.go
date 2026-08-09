package quotaorm

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	authsdk "github.com/hinha/auth-sdks/go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type fakeUsage struct {
	mu sync.Mutex

	reserveErr   error
	confirmErr   error
	releaseErr   error
	reportErr    error
	reservations map[string]*authsdk.UsageReservation
	confirms     []string
	releases     []string
	reports      []authsdk.ReportUsageInput
}

func newFakeUsage() *fakeUsage {
	return &fakeUsage{reservations: map[string]*authsdk.UsageReservation{}}
}

func (f *fakeUsage) ReserveUsage(_ context.Context, _ string, in authsdk.ReserveUsageInput) (*authsdk.UsageReservation, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.reserveErr != nil {
		return nil, f.reserveErr
	}
	if existing, ok := f.reservations[in.IdempotencyKey]; ok {
		return existing, nil
	}
	r := &authsdk.UsageReservation{
		ReservationID:  "res-" + in.IdempotencyKey,
		Status:         authsdk.UsageReservationStatusReserved,
		SubjectType:    in.SubjectType,
		SubjectID:      in.SubjectID,
		DimensionKey:   in.DimensionKey,
		Delta:          in.Delta,
		PeriodKey:      "lifetime",
		IdempotencyKey: in.IdempotencyKey,
	}
	f.reservations[in.IdempotencyKey] = r
	return r, nil
}

func (f *fakeUsage) ConfirmUsage(_ context.Context, _ string, in authsdk.ConfirmUsageInput) (*authsdk.UsageReservation, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.confirmErr != nil {
		return nil, f.confirmErr
	}
	f.confirms = append(f.confirms, in.ReservationID)
	return &authsdk.UsageReservation{ReservationID: in.ReservationID, Status: authsdk.UsageReservationStatusConfirmed}, nil
}

func (f *fakeUsage) ReleaseUsage(_ context.Context, _ string, in authsdk.ReleaseUsageInput) (*authsdk.UsageReservation, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.releaseErr != nil {
		return nil, f.releaseErr
	}
	f.releases = append(f.releases, in.ReservationID)
	return &authsdk.UsageReservation{ReservationID: in.ReservationID, Status: authsdk.UsageReservationStatusReleased}, nil
}

func (f *fakeUsage) ReportUsage(_ context.Context, _ string, in authsdk.ReportUsageInput) ([]authsdk.UsageMeter, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.reportErr != nil {
		return nil, f.reportErr
	}
	f.reports = append(f.reports, in)
	return []authsdk.UsageMeter{}, nil
}

func openTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	require.NoError(t, err)
	return db
}

func TestRunQuotaMutation_SuccessAndConfirm(t *testing.T) {
	db := openTestDB(t)
	fake := newFakeUsage()
	m, err := New(fake, db, WithAPIKey("sa_test"))
	require.NoError(t, err)
	require.NoError(t, m.AutoMigrate(context.Background()))

	type row struct {
		ID   uint `gorm:"primaryKey"`
		Name string
	}
	require.NoError(t, db.AutoMigrate(&row{}))

	err = m.RunQuotaMutation(context.Background(), Mutation{
		IdempotencyKey: "ns:1:namespace_count",
		SubjectType:    "user",
		SubjectID:      "5",
		Dimension:      "namespace_count",
		Delta:          1,
	}, func(tx *gorm.DB) error {
		return tx.Create(&row{Name: "acme"}).Error
	})
	require.NoError(t, err)

	var count int64
	require.NoError(t, db.Model(&row{}).Count(&count).Error)
	assert.Equal(t, int64(1), count)

	// Sync confirm runs inside RunQuotaMutation; outbox should already be published.
	fake.mu.Lock()
	assert.Equal(t, []string{"res-ns:1:namespace_count"}, fake.confirms)
	fake.mu.Unlock()

	var ob QuotaOutbox
	require.NoError(t, db.Where("action = ?", outboxActionConfirm).First(&ob).Error)
	assert.NotNil(t, ob.PublishedAt)
}

func TestRunQuotaMutation_ReserveFailNoWrite(t *testing.T) {
	db := openTestDB(t)
	fake := newFakeUsage()
	fake.reserveErr = errors.New("quota exceeded")
	m, err := New(fake, db)
	require.NoError(t, err)
	require.NoError(t, m.AutoMigrate(context.Background()))

	type row struct {
		ID   uint `gorm:"primaryKey"`
		Name string
	}
	require.NoError(t, db.AutoMigrate(&row{}))

	err = m.RunQuotaMutation(context.Background(), Mutation{
		IdempotencyKey: "k", Dimension: "namespace_count", Delta: 1,
	}, func(tx *gorm.DB) error {
		return tx.Create(&row{Name: "x"}).Error
	})
	require.Error(t, err)

	var count int64
	require.NoError(t, db.Model(&row{}).Count(&count).Error)
	assert.Equal(t, int64(0), count)
}

func TestRunQuotaMutation_TXFailReleases(t *testing.T) {
	db := openTestDB(t)
	fake := newFakeUsage()
	m, err := New(fake, db, WithAPIKey("sa_test"))
	require.NoError(t, err)
	require.NoError(t, m.AutoMigrate(context.Background()))

	err = m.RunQuotaMutation(context.Background(), Mutation{
		IdempotencyKey: "fail-tx", Dimension: "api_key_count", Delta: 1,
	}, func(tx *gorm.DB) error {
		return errors.New("boom")
	})
	require.ErrorContains(t, err, "boom")

	fake.mu.Lock()
	assert.Equal(t, []string{"res-fail-tx"}, fake.releases)
	assert.Empty(t, fake.confirms)
	fake.mu.Unlock()
}

func TestProcessOutbox_RetryThenDeadLetter(t *testing.T) {
	db := openTestDB(t)
	fake := newFakeUsage()
	fake.confirmErr = errors.New("auth down")
	m, err := New(fake, db, WithAPIKey("sa_test"))
	require.NoError(t, err)
	m.maxAttempts = 2
	m.pollEvery = time.Millisecond
	require.NoError(t, m.AutoMigrate(context.Background()))

	// Sync confirm fails; outbox stays unpublished for worker retry.
	require.NoError(t, m.RunQuotaMutation(context.Background(), Mutation{
		IdempotencyKey: "retry", Dimension: "namespace_count", Delta: 1,
	}, func(tx *gorm.DB) error { return nil }))

	require.NoError(t, m.ProcessOutboxOnce(context.Background()))
	// bump available_at for immediate retry
	require.NoError(t, db.Model(&QuotaOutbox{}).Where("1=1").Update("available_at", time.Now().UTC().Add(-time.Second)).Error)
	require.NoError(t, m.ProcessOutboxOnce(context.Background()))

	var row QuotaOutbox
	require.NoError(t, db.Where("action = ?", outboxActionConfirm).First(&row).Error)
	assert.NotNil(t, row.DeadLetteredAt)
}

func TestReconcileSet(t *testing.T) {
	db := openTestDB(t)
	fake := newFakeUsage()
	m, err := New(fake, db, WithAPIKey("sa_test"))
	require.NoError(t, err)

	require.NoError(t, m.ReconcileSet(context.Background(), "user", "2", "namespace_count", 3))
	fake.mu.Lock()
	require.Len(t, fake.reports, 1)
	assert.Equal(t, "namespace_count", fake.reports[0].Items[0].DimensionKey)
	assert.Equal(t, float64(3), fake.reports[0].Items[0].Value)
	assert.Equal(t, authsdk.UsageReportModeSet, fake.reports[0].Items[0].Mode)
	fake.mu.Unlock()
}

func TestChaos_ParallelReserveThenOneTXFails(t *testing.T) {
	db := openTestDB(t)
	fake := newFakeUsage()
	m, err := New(fake, db, WithAPIKey("sa_test"))
	require.NoError(t, err)
	require.NoError(t, m.AutoMigrate(context.Background()))

	var wg sync.WaitGroup
	errs := make(chan error, 2)
	wg.Add(2)
	go func() {
		defer wg.Done()
		errs <- m.RunQuotaMutation(context.Background(), Mutation{
			IdempotencyKey: "p1", Dimension: "namespace_count", Delta: 1, SubjectType: "user", SubjectID: "1",
		}, func(tx *gorm.DB) error { return nil })
	}()
	go func() {
		defer wg.Done()
		errs <- m.RunQuotaMutation(context.Background(), Mutation{
			IdempotencyKey: "p2", Dimension: "namespace_count", Delta: 1, SubjectType: "user", SubjectID: "1",
		}, func(tx *gorm.DB) error { return errors.New("crash mid-tx") })
	}()
	wg.Wait()
	close(errs)

	var okN, failN int
	for e := range errs {
		if e == nil {
			okN++
		} else {
			failN++
		}
	}
	assert.Equal(t, 1, okN)
	assert.Equal(t, 1, failN)

	fake.mu.Lock()
	assert.Len(t, fake.confirms, 1)
	assert.Len(t, fake.releases, 1)
	fake.mu.Unlock()
}

func TestChaos_DoubleSubmitSameIdempotency(t *testing.T) {
	db := openTestDB(t)
	fake := newFakeUsage()
	m, err := New(fake, db, WithAPIKey("sa_test"))
	require.NoError(t, err)
	require.NoError(t, m.AutoMigrate(context.Background()))

	mut := Mutation{IdempotencyKey: "same", Dimension: "namespace_count", Delta: 1, SubjectType: "user", SubjectID: "1"}
	require.NoError(t, m.RunQuotaMutation(context.Background(), mut, func(tx *gorm.DB) error { return nil }))
	// Second call reuses Auth idempotent reserve; local TX + confirm again (idempotent confirm).
	require.NoError(t, m.RunQuotaMutation(context.Background(), mut, func(tx *gorm.DB) error { return nil }))

	fake.mu.Lock()
	assert.Len(t, fake.reservations, 1)
	assert.GreaterOrEqual(t, len(fake.confirms), 1)
	fake.mu.Unlock()
}
