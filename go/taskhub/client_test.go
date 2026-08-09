package taskhub

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestNew_RequiresBaseURL(t *testing.T) {
	if _, err := New(""); err == nil {
		t.Fatal("expected error for empty baseURL")
	}
}

func TestCreateGetCancel(t *testing.T) {
	var sawCreate, sawGet, sawCancel bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-API-Key") != "sa_test" {
			t.Fatalf("api key = %q", r.Header.Get("X-API-Key"))
		}
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/v1/tasks":
			sawCreate = true
			var in CreateTaskInput
			if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
				t.Fatal(err)
			}
			if in.Type != "memoo.episode_ingest" || in.OwnerService != "memoo" {
				t.Fatalf("create body = %+v", in)
			}
			w.WriteHeader(http.StatusAccepted)
			_, _ = w.Write([]byte(`{"message":"accepted","data":{"id":"t1","type":"memoo.episode_ingest","state":"pending","owner_service":"memoo"},"errors":[],"code":"TASK-HUB-202"}`))
		case r.Method == http.MethodGet && r.URL.Path == "/v1/tasks/t1":
			sawGet = true
			_, _ = w.Write([]byte(`{"message":"OK","data":{"id":"t1","type":"memoo.episode_ingest","state":"active","owner_service":"memoo"},"errors":[],"code":"TASK-HUB-200"}`))
		case r.Method == http.MethodPost && r.URL.Path == "/v1/tasks/t1/cancel":
			sawCancel = true
			_, _ = w.Write([]byte(`{"message":"cancelled","data":null,"errors":[],"code":"TASK-HUB-200"}`))
		default:
			t.Fatalf("unexpected %s %s", r.Method, r.URL.Path)
		}
	}))
	defer srv.Close()

	cl, err := New(srv.URL, WithAPIKey("sa_test"))
	if err != nil {
		t.Fatal(err)
	}
	task, err := cl.CreateTask(context.Background(), CreateTaskInput{
		Type:         "memoo.episode_ingest",
		OwnerService: "memoo",
		Payload:      json.RawMessage(`{"namespace":"testing"}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	if task.ID != "t1" {
		t.Fatalf("id = %s", task.ID)
	}
	got, err := cl.GetTask(context.Background(), "t1")
	if err != nil {
		t.Fatal(err)
	}
	if got.State != "active" {
		t.Fatalf("state = %s", got.State)
	}
	if err := cl.CancelTask(context.Background(), "t1"); err != nil {
		t.Fatal(err)
	}
	if !sawCreate || !sawGet || !sawCancel {
		t.Fatalf("saw create=%v get=%v cancel=%v", sawCreate, sawGet, sawCancel)
	}
}

func TestListTasks(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/v1/tasks" {
			t.Fatalf("unexpected %s %s", r.Method, r.URL.Path)
		}
		q := r.URL.Query()
		if q.Get("owner_service") != "memoo" || q.Get("state") != "pending" || q.Get("owner_user_id") != "7" {
			t.Fatalf("query = %v", q)
		}
		_, _ = w.Write([]byte(`{"message":"OK","data":[{"id":"t1","type":"memoo.episode_ingest","state":"pending","owner_service":"memoo"}],"metadata":{"pagination":{"page":1,"limit":20,"total":1,"next_page":false,"prev_page":false,"max_page":1}},"errors":[],"code":"TASK-HUB-200"}`))
	}))
	defer srv.Close()

	cl, err := New(srv.URL, WithAPIKey("sa_test"))
	if err != nil {
		t.Fatal(err)
	}
	uid := uint(7)
	out, err := cl.ListTasks(context.Background(), ListTasksInput{
		OwnerService: "memoo",
		State:        "pending",
		OwnerUserID:  &uid,
		Limit:        20,
	})
	if err != nil {
		t.Fatal(err)
	}
	if out.Total != 1 || len(out.Tasks) != 1 || out.Tasks[0].ID != "t1" {
		t.Fatalf("out = %+v", out)
	}
}

func TestOwnerReportPaths(t *testing.T) {
	var paths []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Task-Hub-Owner-Token") != "th_owner" {
			t.Fatalf("owner token = %q", r.Header.Get("X-Task-Hub-Owner-Token"))
		}
		paths = append(paths, r.URL.Path)
		body, _ := io.ReadAll(r.Body)
		switch {
		case strings.HasSuffix(r.URL.Path, "/heartbeat"):
			if !strings.Contains(string(body), "progress_message") {
				t.Fatalf("heartbeat body = %s", body)
			}
		case strings.HasSuffix(r.URL.Path, "/complete"):
			if !strings.Contains(string(body), "episode_id") {
				t.Fatalf("complete body = %s", body)
			}
		case strings.HasSuffix(r.URL.Path, "/fail"):
			if !strings.Contains(string(body), `"retryable":true`) {
				t.Fatalf("fail body = %s", body)
			}
		}
		_, _ = w.Write([]byte(`{"message":"OK","data":{"id":"t1"},"errors":[],"code":"TASK-HUB-200"}`))
	}))
	defer srv.Close()

	cl, err := New(srv.URL, WithOwnerToken("th_owner"))
	if err != nil {
		t.Fatal(err)
	}
	if err := cl.Heartbeat(context.Background(), "t1", HeartbeatInput{ProgressMessage: "working"}); err != nil {
		t.Fatal(err)
	}
	if err := cl.Complete(context.Background(), "t1", json.RawMessage(`{"episode_id":"e1"}`)); err != nil {
		t.Fatal(err)
	}
	if err := cl.Fail(context.Background(), "t2", "boom", true); err != nil {
		t.Fatal(err)
	}
	want := []string{"/v1/tasks/t1/heartbeat", "/v1/tasks/t1/complete", "/v1/tasks/t2/fail"}
	if len(paths) != len(want) {
		t.Fatalf("paths = %v", paths)
	}
	for i := range want {
		if paths[i] != want[i] {
			t.Fatalf("paths[%d] = %s want %s", i, paths[i], want[i])
		}
	}
}

func TestAPIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusConflict)
		_, _ = w.Write([]byte(`{"message":"conflict"}`))
	}))
	defer srv.Close()

	cl, err := New(srv.URL, WithOwnerToken("tok"))
	if err != nil {
		t.Fatal(err)
	}
	err = cl.Complete(context.Background(), "t1", nil)
	apiErr, ok := err.(*APIError)
	if !ok {
		t.Fatalf("err type %T: %v", err, err)
	}
	if apiErr.StatusCode != http.StatusConflict {
		t.Fatalf("status = %d", apiErr.StatusCode)
	}
}
