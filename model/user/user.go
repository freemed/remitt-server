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

// FromContext returns the UserModel value stored in ctx, if any.
func FromContext(ctx context.Context) (*model.UserModel, bool) {
        if ctx == nil || ctx.Value(userKey) == nil {
                log.Printf("user.FromContext(): nil context or user key: %#v", ctx)
                return &model.UserModel{}, false
        }
        u, ok := ctx.Value(userKey).(*model.UserModel)
        if !ok {
                x, ok := ctx.Value(userKey).(model.UserModel)
                return &x, ok
        }
        return u, ok
}

