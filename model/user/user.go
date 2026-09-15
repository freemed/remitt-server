package user

import (
	"context"
	"log"

	"github.com/freemed/remitt-server/model"
)

// key is an unexported type for keys defined in this package.
// This prevents collisions with keys defined in other packages.
type key int

// userKey is the key for User values in Contexts.  It is
// unexported; clients use user.NewContext and user.FromContext
// instead of using this key directly.
var userKey key = 0

// NewContext returns a new Context that carries value u.
func NewContext(ctx context.Context, u *model.UserModel) context.Context {
	return context.WithValue(ctx, userKey, u)
}

// FromContext returns the UserModel value stored in ctx, if any. A context
// holding a typed nil *model.UserModel is a MISS, not a hit: the assertion
// accepts a nil pointer, so a trusting caller would dereference nil. Every miss
// returns a non-nil, zero-valued *model.UserModel, so a caller that ignores the
// boolean still has something usable.
func FromContext(ctx context.Context) (*model.UserModel, bool) {
	if ctx == nil || ctx.Value(userKey) == nil {
		log.Printf("user.FromContext(): nil context or user key: %#v", ctx)
		return &model.UserModel{}, false
	}
	if u, ok := ctx.Value(userKey).(*model.UserModel); ok {
		if u == nil {
			log.Printf("user.FromContext(): nil *model.UserModel in context")
			return &model.UserModel{}, false
		}
		return u, true
	}
	if x, ok := ctx.Value(userKey).(model.UserModel); ok {
		return &x, true
	}
	return &model.UserModel{}, false
}
