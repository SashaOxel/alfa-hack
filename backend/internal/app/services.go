package app

import (
	"google.golang.org/grpc"

	"github.com/sashaoxel/alfa-hack/backend/internal/adapters/mlclient"
)

// services — зависимости, из которых модули собирают свои gRPC-сервисы.
type services struct {
	ML mlclient.Client
}

// registerServices регистрирует реализации сервисов на gRPC-сервере.
// Пока модулей нет: gateway отвечает 501 на любой публичный метод.
// Каждый модуль добавляет сюда одну строку: corev1.RegisterXServiceServer(s, x.NewHandler(...)).
func registerServices(_ *grpc.Server, _ services) {}
