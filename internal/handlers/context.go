package handlers

import "context"

// private type for context keys set by the HTTP auth middleware
// they can't collide with keys set by other packages
type ctxKey string

const userIDKey ctxKey = "userID"

// WithUserID stores the authenticated user's ID JWT sub on ctx
// handlers that need it can read identity without an import cycle
func WithUserID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, userIDKey, id)
}

// bool is false on public routes
func UserID(ctx context.Context) (string, bool) {
	id, ok := ctx.Value(userIDKey).(string)
	return id, ok
}
