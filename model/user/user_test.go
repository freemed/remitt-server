// Package user: context plumbing for the authenticated user.
//
// # Scope
//
// This package is pure: it stores a *model.UserModel in a context.Context and
// reads it back. There is no database, no network and no I/O, so every branch is
// testable here - notably the two "not found" paths, which callers hit on
// unauthenticated requests.
//
// # What is pinned
//
//   - NewContext/FromContext round-trip a pointer unchanged, and derived
//     contexts (WithCancel and friends) keep the value.
//   - A model.UserModel stored BY VALUE is also accepted and comes back as a
//     pointer to a copy (FromContext's second type switch arm).
//   - A miss returns a non-nil, zero-valued *model.UserModel and false, so
//     callers that ignore the boolean still have something usable.
//   - A nil context, a context without the key, and a context holding an
//     unrelated type are all misses rather than panics.
//   - The key is an unexported named type, so a plain `int` key with the same
//     numeric value cannot collide with it (the anti-collision property
//     documented at user.go:10-17).
//
// # Boundary pinned here (this used to be a documented defect)
//
// NewContext accepts a nil *model.UserModel and FromContext reports it as a
// MISS, not a found value: the type assertion would succeed for a typed nil
// pointer, so the boolean alone cannot be trusted and a caller that checks only
// it would dereference nil. TestFromContextReportsNilUserAsFound (historical
// name: the test used to pin the found=true result).
package user

import (
	"context"
	"testing"

	"github.com/freemed/remitt-server/model"
)

func TestNewContextAndFromContext(t *testing.T) {
	t.Run("pointer_round_trip", func(t *testing.T) {
		want := &model.UserModel{Id: 7, Username: "bob", Role: "admin"}
		ctx := NewContext(context.Background(), want)

		got, ok := FromContext(ctx)
		if !ok {
			t.Fatal("FromContext reported not found for a value stored by NewContext")
		}
		if got != want {
			t.Errorf("FromContext returned a different pointer (%p != %p); the value must not be copied", got, want)
		}
		if got.Id != 7 || got.Username != "bob" || got.Role != "admin" {
			t.Errorf("value = %+v", got)
		}
	})

	t.Run("value_is_preserved_through_derived_contexts", func(t *testing.T) {
		want := &model.UserModel{Id: 1, Username: "bob"}
		base := NewContext(context.Background(), want)

		ctx, cancel := context.WithCancel(base)
		defer cancel()
		if got, ok := FromContext(ctx); !ok || got != want {
			t.Errorf("WithCancel-derived context lost the user: %+v, %v", got, ok)
		}

		if got, ok := FromContext(context.WithValue(ctx, struct{ X int }{}, "noise")); !ok || got != want {
			t.Errorf("an unrelated value shadowed the user: %+v, %v", got, ok)
		}
	})

	t.Run("struct_value_is_wrapped_in_a_pointer", func(t *testing.T) {
		// FromContext's second arm accepts a model.UserModel stored by value.
		ctx := context.WithValue(context.Background(), userKey, model.UserModel{Id: 9, Username: "alice"})
		got, ok := FromContext(ctx)
		if !ok {
			t.Fatal("a by-value model.UserModel must be found")
		}
		if got == nil {
			t.Fatal("FromContext returned nil with ok=true")
		}
		if got.Id != 9 || got.Username != "alice" {
			t.Errorf("value = %+v, want a copy of the stored model", got)
		}
	})

	t.Run("misses_return_a_zero_model_and_false", func(t *testing.T) {
		cases := []struct {
			name string
			ctx  context.Context
		}{
			{name: "background", ctx: context.Background()},
			{name: "todo", ctx: context.TODO()},
			{name: "nil_context", ctx: nil},
			{name: "empty_value_context", ctx: context.WithValue(context.Background(), struct{ Y string }{"k"}, 1)},
			{name: "wrong_type_at_the_user_key", ctx: context.WithValue(context.Background(), userKey, "not a user")},
			{name: "nil_interface_at_the_user_key", ctx: context.WithValue(context.Background(), userKey, nil)},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				got, ok := FromContext(tc.ctx)
				if ok {
					t.Fatalf("expected a miss, got %+v", got)
				}
				if got == nil {
					t.Fatal("a miss must still return a usable (non-nil) model, not nil")
				}
				if *got != (model.UserModel{}) {
					t.Errorf("value = %+v, want the zero model", got)
				}
			})
		}
	})

	t.Run("returned_zero_model_is_safe_to_use", func(t *testing.T) {
		u, ok := FromContext(context.Background())
		if ok {
			t.Fatal("expected a miss")
		}
		// Exactly what a caller would do with it.
		if u.Username != "" || u.Id != 0 || u.Role != "" {
			t.Errorf("zero model has non-zero fields: %+v", u)
		}
		if u.UniqueId() != any(int64(0)) {
			t.Errorf("UniqueId of the zero model = %#v", u.UniqueId())
		}
	})
}

func TestUserKeyIsATypedKey(t *testing.T) {
	// user.go:10-17 declares an unexported named type so that a caller's own key
	// cannot collide, even when the numeric value is the same as userKey's.
	u := &model.UserModel{Id: 99, Username: "attacker"}

	ctx := context.WithValue(context.Background(), 0, u) // plain int key, value 0
	got, ok := FromContext(ctx)
	if ok {
		t.Fatalf("a plain int key collided with the user key: %+v", got)
	}
	if got.Id != 0 {
		t.Errorf("value = %+v, want the zero model", got)
	}

	// A different named type with the same value is equally safe.
	type otherKey int
	ctx2 := context.WithValue(context.Background(), otherKey(0), u)
	if _, ok := FromContext(ctx2); ok {
		t.Error("a different named key type collided with the user key")
	}

	// The same typed key does retrieve it, which proves the test above is
	// exercising the type and not, say, a missing value.
	ctx3 := context.WithValue(context.Background(), userKey, u)
	if got, ok := FromContext(ctx3); !ok || got != u {
		t.Errorf("the unexported key failed to retrieve the value: %+v, %v", got, ok)
	}
}

func TestFromContextReportsNilUserAsFound(t *testing.T) {
	// A typed nil *model.UserModel asserts successfully, so the boolean alone
	// cannot be trusted: FromContext reports it as a MISS and hands back the
	// same non-nil, zero-valued model every other miss returns, so a caller that
	// checks only the boolean no longer dereferences nil. (Historical name: this
	// test used to pin the found=true result.)
	var nilUser *model.UserModel
	ctx := NewContext(context.Background(), nilUser)

	got, ok := FromContext(ctx)
	if ok {
		t.Fatalf("a nil user was reported as found: %#v", got)
	}
	if got == nil {
		t.Fatal("a miss must still return a usable (non-nil) model, not nil")
	}
	if *got != (model.UserModel{}) {
		t.Errorf("value = %#v, want the zero model", got)
	}

	// NewContext(ctx, nil) stores the same typed nil pointer and behaves the
	// same way.
	ctx2 := NewContext(context.TODO(), nil)
	got2, ok2 := FromContext(ctx2)
	if ok2 {
		t.Errorf("NewContext(ctx, nil) -> %#v, %v; want a miss", got2, ok2)
	}
	if got2 == nil || *got2 != (model.UserModel{}) {
		t.Errorf("NewContext(ctx, nil) -> %#v; want a usable zero model", got2)
	}
}

func TestNewContextDoesNotMutateTheModel(t *testing.T) {
	u := &model.UserModel{Id: 5, Username: "bob", Role: "admin"}
	ctx := NewContext(context.Background(), u)

	other := NewContext(context.Background(), &model.UserModel{Id: 6, Username: "alice"})

	got, _ := FromContext(ctx)
	if got.Id != 5 || got.Username != "bob" {
		t.Errorf("the first context was affected by a second NewContext: %+v", got)
	}
	gotOther, _ := FromContext(other)
	if gotOther.Id != 6 || gotOther.Username != "alice" {
		t.Errorf("the second context holds the wrong user: %+v", gotOther)
	}
	if got == gotOther {
		t.Error("two NewContext calls returned the same pointer for different users")
	}
}
