package version

import (
	"github.com/rmdashrf/rclone_acd_hack/cmd"
	"github.com/spf13/cobra"
)

func init() {
	cmd.Root.AddCommand(commandDefintion)
}

var commandDefintion = &cobra.Command{
	Use:   "version",
	Short: `Show the version number.`,
	Run: func(command *cobra.Command, args []string) {
		cmd.CheckArgs(0, 0, command, args)
		cmd.ShowVersion()
	},
}
