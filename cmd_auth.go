package main

import (
	"context"
	"fmt"

	"github.com/ginkio-nl/abmctl/internal/apiclient"
)

// AuthCmd groups authentication-related utility commands.
type AuthCmd struct {
	Test AuthTestCmd `cmd:"" name:"test" help:"Fetch an access token and confirm your credentials work, without calling any business endpoint."`
}

// AuthTestCmd exercises the full JWT-assertion -> token-exchange flow and
// reports success/failure, so credential problems can be diagnosed without
// guessing at an unrelated API error.
type AuthTestCmd struct{}

func (c *AuthTestCmd) Run(g *Globals, client *apiclient.Client) error {
	token, err := client.Tokens.AccessToken(context.Background())
	if err != nil {
		return err
	}
	suffix := token
	if len(suffix) > 8 {
		suffix = suffix[len(suffix)-8:]
	}
	fmt.Printf("OK: obtained an access token (ends in ...%s)\n", suffix)
	return nil
}
