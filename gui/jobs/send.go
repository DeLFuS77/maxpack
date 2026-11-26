// SPDX-License-Identifier: GPL-3.0-or-later

package jobs

// Sending a .p5z to the console (Details › Send to the console, and the end card of Compress): the engine
// sends it (maxpack send) to MaxPack for the console, which verifies it when it has it whole and only then
// gives it its name. Nothing is written on the PC. If the connection breaks the engine goes on from what
// the console has; cancelled, the console deletes what it got.

import (
	"context"
	"strings"

	"github.com/DeLFuS77/maxpack/gui/core"
)

// SendCodeEnv is how the engine gets the pairing code: in its environment, never on its command line,
// where other programs of the PC could read it.
const SendCodeEnv = core.SendCodeEnv

// PlanSend plans sending each .p5z to the console at o.Console, to o.Drive (the engine's choice when it is
// ""). Out is "" and Need 0: nothing is written on the PC. Anything but a .p5z cannot go ("not_p5z"); without
// the console's address or its code nothing can ("no_console").
func PlanSend(items []Item, o Options) []Plan {
	plans := make([]Plan, len(items))
	for i, it := range items {
		p := Plan{Item: it}
		switch {
		case o.Console == "" || o.ConsoleCode == "":
			p.Block = "no_console"
		case !isP5z(it):
			p.Block = "not_p5z"
		default:
			p.Args = []string{"send", it.Path, o.Console}
			if o.Drive != "" {
				p.Args = append(p.Args, "--drive", o.Drive)
			}
		}
		plans[i] = p
	}
	return plans
}

// sendFailure is the detail of a send that did not end well: the console's refusals and what the engine
// says of them get send_ codes, which the page says in words of their own (a "busy" console is not a .p5z
// another MaxPack is changing); a .p5z that arrived damaged says what the console found wrong.
func sendFailure(code string, result core.Event) string {
	switch code {
	case "cancelled", "io", "bad_input":
		return code
	case "p5z_busy":
		return "busy"
	}
	if strings.HasPrefix(code, "exit_") || strings.Contains(code, " ") {
		return code // the engine ended without a word, or a message of its own
	}
	detail := "send_" + code
	if code == "verify_failed" {
		var bad []string
		list, _ := result.Raw["bad"].([]any)
		for _, entry := range list {
			if b, ok := entry.(map[string]any); ok {
				if path, _ := b["path"].(string); path != "" {
					bad = append(bad, path)
				}
			}
		}
		if len(bad) > 0 {
			detail += ": " + strings.Join(bad, ", ")
		}
	}
	return detail
}

// sendOne sends a .p5z: sending, then verifying while the console verifies it, and ok ("sent") once the
// console says it is whole and named.
func (r *Runner) sendOne(ctx context.Context, i int, p Plan, o Options) outcome {
	r.item(i, StateSending, "")
	verifying := false
	result, ok, failure := r.engine(ctx, i, p.Args, o, func(e core.Event) {
		if e.Kind == "progress" && e.Phase == "verifying" && !verifying {
			verifying = true
			r.item(i, StateVerifying, "")
		}
	})
	if !ok {
		return outcome{state: endState(false, failure), detail: sendFailure(failure, result)}
	}
	return outcome{state: StateOK, detail: "sent"}
}
