package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

// newStoreCommand shows what rein's store holds, the way state.yaml, the
// ledger and the snapshots were read as files before (D51). It changes
// nothing, so it waits for no change (R1).
func newStoreCommand(environment *commandEnvironment) *cobra.Command {
	return &cobra.Command{
		Use:   "store [document]",
		Short: "Show what rein's store holds: the state, the ledger and the snapshots",
		Long: "store shows rein's own records on this machine, kept encrypted in rein's store, which only\n" +
			"rein opens, so that neither an agent nor a slip of the hand changes them. Without an argument\n" +
			"it lists the documents with their sizes; with a name it prints that document as rein wrote\n" +
			"it: state.yaml, state.yaml.previous, removals.yaml, inserts/<organisation id>.yaml and\n" +
			"snapshots/<name>.json. It changes nothing; rein status is the readable summary, rein reset\n" +
			"machine erases the store.",
		Example: "  rein store\n  rein store state.yaml",
		Args:    cobra.MaximumNArgs(1),
		RunE: func(command *cobra.Command, arguments []string) error {
			orchestrator, err := environment.orchestrate()
			if err != nil {
				return err
			}
			if len(arguments) == 1 {
				content, err := orchestrator.StoreDocument(arguments[0])
				if err != nil {
					return err
				}
				_, err = environment.out.Write(content)
				return err
			}
			where, documents, err := orchestrator.StoreDocuments()
			if err != nil {
				return err
			}
			if len(documents) == 0 {
				fmt.Fprintf(environment.out, "rein's store %s holds nothing yet\n", where)
				return nil
			}
			fmt.Fprintf(environment.out, "rein's store %s holds:\n", where)
			rows := [][]string{{"DOCUMENT", "BYTES"}}
			for _, document := range documents {
				rows = append(rows, []string{document.Name, fmt.Sprint(document.Bytes)})
			}
			printTable(environment, rows)
			return nil
		},
	}
}
