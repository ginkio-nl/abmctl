package main

import (
	"context"
	"fmt"
)

func dispatchAuth(args []string) error {
	if len(args) == 0 || args[0] != "test" {
		return fmt.Errorf("usage: abmctl auth test")
	}
	return runAuthTest(args[1:])
}

// runAuthTest exercises the full JWT-assertion -> token-exchange flow and
// reports success/failure, so credential problems can be diagnosed without
// guessing at an unrelated API error.
func runAuthTest(args []string) error {
	fs, g := newFlagSet("abmctl auth test")
	if err := fs.Parse(args); err != nil {
		return err
	}

	client, err := buildClient(g)
	if err != nil {
		return err
	}

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
