package main

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestServeShutdown(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("ok"))
	})}
	workerExited := make(chan struct{})
	result := make(chan error, 1)
	go func() {
		result <- serve(ctx, server, listener, func(ctx context.Context) error {
			defer close(workerExited)
			<-ctx.Done()
			return ctx.Err()
		}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	}()
	client := &http.Client{Timeout: time.Second}
	response, err := client.Get("http://" + listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatal(response.StatusCode)
	}
	cancel()
	select {
	case err := <-result:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("shutdown did not finish")
	}
	select {
	case <-workerExited:
	default:
		t.Fatal("worker not stopped before returning")
	}
}

func TestServeWorkerFailure(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	err = serve(context.Background(), &http.Server{}, listener, func(context.Context) error {
		return errors.New("private worker payload")
	}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err == nil || strings.Contains(err.Error(), "private") {
		t.Fatalf("unsafe failure: %v", err)
	}
	if connection, err := net.DialTimeout("tcp", listener.Addr().String(), time.Second); err == nil {
		connection.Close()
		t.Fatal("listener still open")
	}
}

func TestServeDrainsActiveRequests(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	requestCtx, cancelRequests := context.WithCancel(context.WithoutCancel(ctx))
	defer cancelRequests()
	started, release := make(chan struct{}), make(chan struct{})
	workerStopped := make(chan struct{})
	server := &http.Server{
		BaseContext: func(net.Listener) context.Context { return requestCtx },
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			close(started)
			select {
			case <-r.Context().Done():
				http.Error(w, "request canceled before drain", http.StatusServiceUnavailable)
			case <-release:
				w.Write([]byte("finished"))
			}
		}),
	}
	result := make(chan error, 1)
	go func() {
		result <- serve(ctx, server, listener, func(ctx context.Context) error {
			<-ctx.Done()
			close(workerStopped)
			return ctx.Err()
		}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	}()
	status := make(chan int, 1)
	go func() {
		client := &http.Client{Timeout: 2 * time.Second}
		response, err := client.Get("http://" + listener.Addr().String())
		if err != nil {
			status <- 0
			return
		}
		defer response.Body.Close()
		io.Copy(io.Discard, response.Body)
		status <- response.StatusCode
	}()
	<-started
	cancel()
	<-workerStopped
	close(release)
	if got := <-status; got != http.StatusOK {
		t.Fatalf("request did not drain: %d", got)
	}
	if err := <-result; err != nil {
		t.Fatal(err)
	}
}
