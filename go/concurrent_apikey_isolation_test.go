package authsdk_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"

	authsdk "github.com/hinha/auth-sdks/go"
	"github.com/hinha/auth-sdks/go/taskhub"
)

func TestConcurrentAuthLoginAndTaskHubCreateTask_APIKeysIsolated(t *testing.T) {
	var authWrong, hubWrong atomic.Int64
	authSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-API-Key") != "sa_memoo" {
			authWrong.Add(1)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"message":"ok","code":"200","data":{"access_token":"a","refresh_token":"r","token_type":"Bearer","expires_in":3600,"session_id":"s"},"errors":null}`)
	}))
	t.Cleanup(authSrv.Close)

	hubSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-API-Key") != "sa_taskhub" {
			hubWrong.Add(1)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"data":{"id":"t1","state":"queued","type":"x","owner_service":"memoo"}}`)
	}))
	t.Cleanup(hubSrv.Close)

	authCl, err := authsdk.New(authSrv.URL, "memoo", authsdk.Credentials("sa_memoo"))
	if err != nil {
		t.Fatal(err)
	}
	hubCl, err := taskhub.New(hubSrv.URL, taskhub.WithAPIKey("sa_taskhub"))
	if err != nil {
		t.Fatal(err)
	}

	var wg sync.WaitGroup
	for i := 0; i < 200; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			_, _ = authCl.Login(context.Background(), authsdk.LoginInput{Email: "a@b.c", Password: "x"})
		}()
		go func() {
			defer wg.Done()
			_, _ = hubCl.CreateTask(context.Background(), taskhub.CreateTaskInput{Type: "memoo.episode_ingest", OwnerService: "memoo"})
		}()
	}
	wg.Wait()

	if authWrong.Load() != 0 || hubWrong.Load() != 0 {
		t.Fatalf("key cross-contamination: authWrong=%d hubWrong=%d", authWrong.Load(), hubWrong.Load())
	}
}
