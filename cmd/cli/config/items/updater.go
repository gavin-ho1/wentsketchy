package items

import (
	"context"

	"github.com/lucax88x/wentsketchy/cmd/cli/config/args"
)

type Updater interface {
	Update(ctx context.Context, args *args.In) error
}
