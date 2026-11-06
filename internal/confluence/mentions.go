package confluence

import (
	"context"
	"sync"

	"github.com/zhani/atlassian-cli/internal/htmlconv"
)

// UserGetter looks users up; *Client implements it.
type UserGetter interface {
	GetUser(ctx context.Context, by, value string) (*User, error)
}

// Mentions maps the users a page mentions to their names.
type Mentions map[htmlconv.UserRef]htmlconv.User

// ResolveMentions looks up every user the storage mentions, in parallel.
// Users that cannot be looked up are left out (rendered as unknown).
func ResolveMentions(ctx context.Context, g UserGetter, storage string) Mentions {
	refs := htmlconv.Mentions(storage)
	out := Mentions{}
	if g == nil || len(refs) == 0 {
		return out
	}
	var mu sync.Mutex
	var wg sync.WaitGroup
	sem := make(chan struct{}, 8)
	for _, r := range refs {
		by, value := "key", r.Key
		switch {
		case r.AccountID != "":
			by, value = "accountId", r.AccountID
		case r.Key == "":
			by, value = "username", r.Username
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			u, err := g.GetUser(ctx, by, value)
			if err != nil || u.DisplayName == "" {
				return
			}
			mu.Lock()
			out[r] = htmlconv.User{DisplayName: u.DisplayName, Email: u.Email}
			mu.Unlock()
		}()
	}
	wg.Wait()
	return out
}

// Options is the htmlconv configuration that renders these mentions.
func (m Mentions) Options() htmlconv.Options {
	return htmlconv.Options{User: func(r htmlconv.UserRef) (htmlconv.User, bool) {
		u, ok := m[r]
		return u, ok
	}}
}
