package wallet

import (
	"context"
	"io"
)

type PrintOutput interface {
	Print(out io.Writer)
}

type ServiceRule[T PrintOutput] interface {
	Name() string
	Analyse(ctx context.Context, wl *Wallet) (T, error)
}
