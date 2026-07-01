package mirror

import (
	"context"
	"errors"
	"fmt"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing/transport"
	"github.com/go-git/go-git/v5/plumbing/transport/http"
	"github.com/go-git/go-git/v5/storage/memory"
)

type Client struct{}

func New() *Client {
	return &Client{}
}

func (c *Client) GetRefs(ctx context.Context, url, user, token string) (map[string]string, error) {
	ep, err := transport.NewEndpoint(url)
	if err != nil {
		return nil, fmt.Errorf("parse endpoint: %w", err)
	}
	ep.User = user

	r := git.NewRemote(memory.NewStorage(), &config.RemoteConfig{
		Name: "origin",
		URLs: []string{url},
	})

	var auth transport.AuthMethod
	if user != "" {
		auth = &http.BasicAuth{Username: user, Password: token}
	}

	refs, err := r.ListContext(ctx, &git.ListOptions{Auth: auth})
	if err != nil {
		return nil, fmt.Errorf("list remote: %w", err)
	}

	result := make(map[string]string, len(refs))
	for _, ref := range refs {
		result[ref.Name().String()] = ref.Hash().String()
	}
	return result, nil
}

func (c *Client) MirrorClone(ctx context.Context, url, user, token, dest string) error {
	var auth transport.AuthMethod
	if user != "" {
		auth = &http.BasicAuth{Username: user, Password: token}
	}

	_, err := git.PlainCloneContext(ctx, dest, true, &git.CloneOptions{
		URL:  url,
		Auth: auth,
	})
	if err != nil {
		return fmt.Errorf("mirror clone: %w", err)
	}
	return nil
}

func (c *Client) MirrorPush(ctx context.Context, repoDir, url, user, token string) error {
	repo, err := git.PlainOpen(repoDir)
	if err != nil {
		return fmt.Errorf("open repo: %w", err)
	}

	remote, err := repo.CreateRemote(&config.RemoteConfig{
		Name: "mirror-target",
		URLs: []string{url},
	})
	if err != nil {
		existing, err2 := repo.Remote("mirror-target")
		if err2 != nil {
			return fmt.Errorf("create remote: %w", err)
		}
		remote = existing
	}

	var auth transport.AuthMethod
	if user != "" {
		auth = &http.BasicAuth{Username: user, Password: token}
	}

	err = remote.PushContext(ctx, &git.PushOptions{
		Auth:       auth,
		RemoteName: remote.Config().Name,
		RefSpecs:   []config.RefSpec{"+refs/*:refs/*"},
	})
	if err != nil && !errors.Is(err, git.NoErrAlreadyUpToDate) {
		return fmt.Errorf("mirror push: %w", err)
	}
	return nil
}
