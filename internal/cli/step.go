// Package cli wires cobra commands and shared CLI helpers.
package cli

import (
	"context"
	"errors"
	"fmt"

	"xsh/internal/log"
)

// Step is the unit of work executed by the install pipeline.
type Step interface {
	Name() string
	// PreCheck returns nil when the step needs to run, ErrAlreadyDone to skip,
	// or any other error to abort the pipeline.
	PreCheck(ctx context.Context) error
	Do(ctx context.Context) error
	Rollback(ctx context.Context) error
}

// ErrAlreadyDone signals that a Step has nothing to do.
var ErrAlreadyDone = errors.New("step already done")

// Run executes the given steps serially. On Do failure, previously succeeded
// steps are rolled back in reverse order.
func Run(ctx context.Context, steps []Step) error {
	done := make([]Step, 0, len(steps))

	for _, s := range steps {
		log.Info("step start: %s", s.Name())

		if err := s.PreCheck(ctx); err != nil {
			if errors.Is(err, ErrAlreadyDone) {
				log.Info("step skip: %s (already done)", s.Name())
				continue
			}
			log.Error("step precheck failed: %s: %v", s.Name(), err)
			rollback(ctx, done)
			return fmt.Errorf("step %s precheck: %w", s.Name(), err)
		}

		if err := s.Do(ctx); err != nil {
			log.Error("step failed: %s: %v", s.Name(), err)
			rollback(ctx, done)
			return fmt.Errorf("step %s: %w", s.Name(), err)
		}

		log.Info("step done: %s", s.Name())
		done = append(done, s)
	}
	return nil
}

func rollback(ctx context.Context, done []Step) {
	for i := len(done) - 1; i >= 0; i-- {
		s := done[i]
		log.Warn("rollback: %s", s.Name())
		if err := s.Rollback(ctx); err != nil {
			log.Error("rollback failed: %s: %v", s.Name(), err)
		}
	}
}
