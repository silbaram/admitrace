package cli

import (
	"context"
	"errors"

	"github.com/silbaram/admitrace/internal/adapter"
	"github.com/silbaram/admitrace/internal/contract"
	"github.com/silbaram/admitrace/internal/hydration"
)

type commandDependencies struct {
	prepareHydration func(context.Context, hydration.Options) (*adapter.Hydration, error)
}

func defaultCommandDependencies() commandDependencies {
	return commandDependencies{
		prepareHydration: func(ctx context.Context, options hydration.Options) (*adapter.Hydration, error) {
			factory := hydration.NewFactory()
			session, err := factory.Connect(ctx, options)
			if err != nil {
				return nil, err
			}
			if session == nil {
				return nil, &contract.InternalError{Operation: "initialize hydration", Err: errors.New("explicit context returned no session")}
			}
			reader, err := session.NewReader()
			if err != nil {
				return nil, err
			}
			return &adapter.Hydration{Reader: reader, SourceLabel: session.ContextLabel(), ProfileMatch: session.ProfileMatch()}, nil
		},
	}
}
