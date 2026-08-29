package server

import (
	"net/http"

	"github.com/armonic-tech/armonic-backend/pkg/ratelimit"
)

const (
	claimPerIPPerMin  = 5
	claimGlobalPerMin = 20
	claimBurst        = 5

	loginPerIPPerMin      = 20
	loginPerAccountPerMin = 5
	loginBurst            = 5

	signupPerIPPerMin = 20
	signupBurst       = 5

	powChallengePerMin = 60
	powChallengeBurst  = 10
)

type middleware func(http.Handler) http.Handler

func newIPKey(trustedProxies []string) (func(*http.Request) string, error) {
	resolver, err := ratelimit.NewIPResolver(trustedProxies)
	if err != nil {
		return nil, err
	}
	return resolver.Key, nil
}

func perIP(perMinute, burst int, key func(*http.Request) string) middleware {
	return ratelimit.New(perMinute, burst).Middleware(key)
}

func global(perMinute, burst int) middleware {
	return ratelimit.New(perMinute, burst).Middleware(ratelimit.Global("all"))
}

func chain(h http.HandlerFunc, mws ...middleware) http.HandlerFunc {
	var wrapped http.Handler = h
	for i := len(mws) - 1; i >= 0; i-- {
		wrapped = mws[i](wrapped)
	}
	return wrapped.ServeHTTP
}
