package databases

import (
	"context"
	"errors"
	"io"
	"slices"
	"testing"
)

func TestPrepareDockerImage(t *testing.T) {
	for name, engine := range engines {
		t.Run(name, func(t *testing.T) {
			for _, tc := range []struct {
				name                 string
				cached, original     bool
				pullFails, wantError bool
				operations           []string
			}{
				{name: "cached", cached: true, operations: []string{"image"}},
				{name: "missing", operations: []string{"image", "pull"}},
				{name: "pull failure", pullFails: true, wantError: true, operations: []string{"image", "pull"}},
				{name: "original missing", original: true, wantError: true, operations: []string{"image"}},
			} {
				t.Run(tc.name, func(t *testing.T) {
					var operations []string
					run := func(_ context.Context, _ io.Reader, _ io.Writer, args ...string) error {
						if args[len(args)-1] != engine.Image {
							t.Fatalf("comando usou a imagem errada: %v", args)
						}
						operations = append(operations, args[0])
						if (args[0] == "image" && !tc.cached) || (args[0] == "pull" && tc.pullFails) {
							return errors.New("Docker command failed")
						}
						return nil
					}
					err := prepareDockerImage(context.Background(), engine.Image, tc.original, run)
					if (err != nil) != tc.wantError || !slices.Equal(operations, tc.operations) {
						t.Fatalf("comandos %v, erro %v; esperado %v, erro: %t", operations, err, tc.operations, tc.wantError)
					}
				})
			}
		})
	}
}
