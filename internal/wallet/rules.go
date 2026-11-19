package wallet

// A `ServiceRule` allows you define a specific type of rule
// that analyses the state of the expense store for a giving user
// at whatever granularity you desire.
// It can return a fact or none at all or fail the process with an error due
// to an expected invariant not being available.
type ServiceRule[T any] interface {
	// the name of the rule
	Name() string

	// Analyse runs the rule against the current state of your store for a giving user.
	// for giving currency.
	Analyse(wl *Wallet) (T, error)
}
