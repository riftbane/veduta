package cli

import (
	"github.com/riftbane/veduta/v2/internal/dap"
)

func init() {
	register(command{
		name: "dap", usage: "dap", summary: "the debug adapter of Lua games on stdin and stdout (Debug Adapter Protocol), which editors start: breakpoints, stepping, the stack, locals and entities, in the player or a headless scenario",
		run: func(env *Env, _ *Session, args []string) (any, error) {
			if len(args) > 0 {
				return nil, usagef("dap takes no arguments")
			}
			return nil, dap.NewSession(env.Stdin, env.Stdout).Serve()
		},
	})
}
