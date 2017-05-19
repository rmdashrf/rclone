package md5sum

import (
	"os"

	"github.com/rmdashrf/rclone_acd_hack/cmd"
	"github.com/rmdashrf/rclone_acd_hack/fs"
	"github.com/spf13/cobra"
)

func init() {
	cmd.Root.AddCommand(commandDefintion)
}

var commandDefintion = &cobra.Command{
	Use:   "md5sum remote:path",
	Short: `Produces an md5sum file for all the objects in the path.`,
	Long: `
Produces an md5sum file for all the objects in the path.  This
is in the same format as the standard md5sum tool produces.
`,
	Run: func(command *cobra.Command, args []string) {
		cmd.CheckArgs(1, 1, command, args)
		fsrc := cmd.NewFsSrc(args)
		cmd.Run(false, false, command, func() error {
			return fs.Md5sum(fsrc, os.Stdout)
		})
	},
}
