package wallet

import "context"

type ServiceRule[T any] interface {
	Name() string

	Analyse(ctx context.Context, wl *Wallet) (T, error)
}
