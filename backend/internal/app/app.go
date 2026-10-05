// Package app собирает зависимости core руками (DI без фреймворка) и запускает серверы.
package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"time"

	"google.golang.org/grpc"

	corev1 "github.com/sashaoxel/alfa-hack/backend/gen/alfa/core/v1"
	"github.com/sashaoxel/alfa-hack/backend/internal/adapters/mlclient"
	"github.com/sashaoxel/alfa-hack/backend/internal/config"
	"github.com/sashaoxel/alfa-hack/backend/internal/modules/auth"
	"github.com/sashaoxel/alfa-hack/backend/internal/platform/gateway"
	"github.com/sashaoxel/alfa-hack/backend/internal/platform/grpcserver"
)

const shutdownTimeout = 10 * time.Second

// publicMethods доступны без сессии. DemoService.Reset — тоже: сервис сам отвечает NOT_FOUND вне DEMO_MODE.
var publicMethods = []string{
	corev1.AuthService_RequestOtp_FullMethodName,
	corev1.AuthService_VerifyOtp_FullMethodName,
	corev1.AuthService_ListDemoPersonas_FullMethodName,
	corev1.DemoService_Reset_FullMethodName,
}

// Run запускает core и блокируется до отмены ctx, затем корректно останавливается.
func Run(ctx context.Context, cfg config.Config, log *slog.Logger) error {
	if cfg.JWTSecretGenerated {
		log.Warn("JWT_SECRET не задан: сгенерирован на время работы процесса (DEMO_MODE), сессии не переживут рестарт")
	}

	ml, closeML, err := newMLClient(cfg)
	if err != nil {
		return err
	}
	defer closeML()

	tokens := auth.NewTokens(cfg.JWTSecret, cfg.SessionTTL)

	grpcLis, err := net.Listen("tcp", cfg.GRPCAddr)
	if err != nil {
		return fmt.Errorf("app: listen grpc %s: %w", cfg.GRPCAddr, err)
	}
	grpcSrv := grpcserver.New(grpcserver.Options{Logger: log, Tokens: tokens, PublicMethods: publicMethods})
	registerServices(grpcSrv, services{ML: ml})

	// Gateway ходит в gRPC по loopback; для ":9090" берём 127.0.0.1.
	gw, err := gateway.New(ctx, gateway.Options{
		Logger:       log,
		GRPCAddr:     loopbackAddr(grpcLis.Addr()),
		SSEHeartbeat: cfg.SSEHeartbeat,
		Ready: []gateway.ReadyCheck{
			{Name: "ml", Check: func(ctx context.Context) error { _, err := ml.ModelInfo(ctx); return err }},
		},
	})
	if err != nil {
		return err
	}
	defer gw.Close()

	// Контекст SSE-запросов отменяется при остановке, иначе Shutdown ждал бы бесконечные стримы.
	baseCtx, cancelBase := context.WithCancel(context.WithoutCancel(ctx))
	defer cancelBase()
	httpSrv := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           gw,
		ReadHeaderTimeout: 10 * time.Second,
		BaseContext:       func(net.Listener) context.Context { return baseCtx },
		// WriteTimeout намеренно не задан: SSE-ответы длятся неограниченно.
	}

	errc := make(chan error, 2)
	go func() { errc <- serveGRPC(grpcSrv, grpcLis) }()
	go func() {
		if err := httpSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errc <- fmt.Errorf("app: http: %w", err)
		}
	}()
	log.Info("core запущен", "http", cfg.HTTPAddr, "grpc", grpcLis.Addr().String(),
		"ml_client", cfg.MLClient, "demo_mode", cfg.DemoMode)

	var runErr error
	select {
	case <-ctx.Done():
		log.Info("останавливаюсь")
	case runErr = <-errc:
		log.Error("сервер упал", "error", runErr)
	}

	cancelBase()
	shutCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	if err := httpSrv.Shutdown(shutCtx); err != nil {
		log.Warn("http shutdown", "error", err)
	}
	stopGRPC(grpcSrv, shutCtx)
	return runErr
}

func serveGRPC(s *grpc.Server, lis net.Listener) error {
	if err := s.Serve(lis); err != nil && !errors.Is(err, grpc.ErrServerStopped) {
		return fmt.Errorf("app: grpc: %w", err)
	}
	return nil
}

// stopGRPC дожидается завершения вызовов, но не дольше, чем позволяет ctx.
func stopGRPC(s *grpc.Server, ctx context.Context) {
	done := make(chan struct{})
	go func() { s.GracefulStop(); close(done) }()
	select {
	case <-done:
	case <-ctx.Done():
		s.Stop()
	}
}

func newMLClient(cfg config.Config) (mlclient.Client, func(), error) {
	if cfg.MLClient == config.MLClientFake {
		return mlclient.NewFake(), func() {}, nil
	}
	c, err := mlclient.NewGRPC(cfg.MLAddr)
	if err != nil {
		return nil, nil, err
	}
	return c, func() { _ = c.Close() }, nil
}

func loopbackAddr(a net.Addr) string {
	tcp, ok := a.(*net.TCPAddr)
	if !ok {
		return a.String()
	}
	return net.JoinHostPort("127.0.0.1", fmt.Sprint(tcp.Port))
}
