package tmux

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

func (b *TmuxBackend) switchClient(ctx context.Context, target string, clients []clientState) error {
	owner, err := b.navigationOwner(clients)
	if err != nil {
		return err
	}
	// tmux resolves -c by name again; replacement checks apply to the snapshot,
	// not atomically to that later lookup.
	return b.client.Execute(ctx, SwitchClient{Client: owner.name, Target: target})
}

func (b *TmuxBackend) navigationOwner(clients []clientState) (clientIdentity, error) {
	b.navigationMu.Lock()
	defer b.navigationMu.Unlock()
	if b.navigationClient.name != "" {
		for _, current := range clients {
			if !current.observer && current.identity == b.navigationClient {
				return current.identity, nil
			}
		}
		return clientIdentity{}, fmt.Errorf("invoking tmux client %q is no longer attached", b.navigationClient.name)
	}

	owner := invokingClient(clients)
	if owner.identity.name == "" {
		return clientIdentity{}, errors.New("cannot identify a unique invoking tmux client")
	}
	b.navigationClient = owner.identity
	return owner.identity, nil
}

func (b *TmuxBackend) queryClients(ctx context.Context) ([]clientState, error) {
	ctx, cancel := context.WithTimeout(ctx, queryTimeout)
	defer cancel()
	output, err := b.client.RunLimited(ctx, stateOutputLimit, "list-clients", "-F", clientStateFormat)
	if err != nil {
		return nil, err
	}
	if output == "" {
		return nil, nil
	}

	var clients []clientState
	for line := range strings.SplitSeq(strings.TrimSuffix(output, "\n"), "\n") {
		client, err := parseClientLine(line)
		if err != nil {
			return nil, fmt.Errorf("invalid tmux client row: %w", err)
		}
		clients = append(clients, client)
	}
	return clients, nil
}
