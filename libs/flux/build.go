package flux

import (
	"context"
	"errors"
	"io"

	"go.uber.org/zap"
)

type Library struct{}

func Configure(ctx context.Context, logger *zap.Logger) (*Library, error) {
	return &Library{}, nil
}

func (l *Library) Install(ctx context.Context, logger *zap.Logger) error {
	return errors.New("implement me")
}

func (l *Library) WritePackageConfig(w io.Writer) error {
	return errors.New("implement me")
}
