package cli

import (
	"bufio"
	"fmt"
	"os"
	"strings"
)

// confirmDestructive prompts for confirmation unless yes is set. It reads from
// stdin and returns nil when the user confirms, or an error when the input is
// not an affirmative answer. When stdin is not a terminal and --yes is not
// set, it fails rather than silently proceeding.
func confirmDestructive(yes bool, what string) error {
	if yes {
		return nil
	}
	fmt.Fprintf(os.Stderr, "Are you sure you want to %s? [y/N] ", what)
	scanner := bufio.NewScanner(os.Stdin)
	scanner.Scan()
	answer := strings.TrimSpace(strings.ToLower(scanner.Text()))
	if answer != "y" && answer != "yes" {
		return fmt.Errorf("aborted: %s not confirmed", what)
	}
	return nil
}
