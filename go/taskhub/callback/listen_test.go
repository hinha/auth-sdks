package callback_test

import (
	"context"
	"net"
	"strconv"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"github.com/hinha/auth-sdks/go/taskhub/callback"
	callbackv1 "github.com/hinha/auth-sdks/go/taskhub/callback/v1"
)

type stubServer struct {
	callbackv1.UnimplementedTaskCallbackServer
}

func (s *stubServer) Dispatch(ctx context.Context, req *callbackv1.DispatchRequest) (*callbackv1.DispatchResponse, error) {
	return &callbackv1.DispatchResponse{
		Status:  callbackv1.DispatchStatus_DISPATCH_STATUS_ACCEPTED,
		Message: "ok " + req.GetTaskId(),
	}, nil
}

func TestListen_NilWhenDisabled(t *testing.T) {
	l, err := callback.Listen(0, &stubServer{})
	if err != nil || l != nil {
		t.Fatalf("got (%v, %v)", l, err)
	}
}

func TestListen_Dispatch(t *testing.T) {
	tmp, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := tmp.Addr().(*net.TCPAddr).Port
	_ = tmp.Close()

	l, err := callback.Listen(port, &stubServer{})
	if err != nil {
		t.Fatal(err)
	}
	defer l.GracefulStop()

	deadline := time.Now().Add(3 * time.Second)
	var conn *grpc.ClientConn
	for {
		conn, err = grpc.NewClient(
			net.JoinHostPort("127.0.0.1", strconv.Itoa(port)),
			grpc.WithTransportCredentials(insecure.NewCredentials()),
		)
		if err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("dial: %v", err)
		}
		time.Sleep(20 * time.Millisecond)
	}
	defer conn.Close()

	client := callbackv1.NewTaskCallbackClient(conn)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	resp, err := client.Dispatch(ctx, &callbackv1.DispatchRequest{TaskId: "t1", Type: "memoo.episode_ingest"})
	if err != nil {
		t.Fatal(err)
	}
	if resp.GetStatus() != callbackv1.DispatchStatus_DISPATCH_STATUS_ACCEPTED {
		t.Fatalf("status = %v", resp.GetStatus())
	}
	if resp.GetMessage() != "ok t1" {
		t.Fatalf("message = %q", resp.GetMessage())
	}
}
