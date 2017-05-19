package obscure

import (
	"fmt"

	"github.com/rmdashrf/rclone_acd_hack/cmd"
	"github.com/rmdashrf/rclone_acd_hack/fs"
	"github.com/spf13/cobra"
)

func init() {
	cmd.Root.AddCommand(commandDefintion)
}

var commandDefintion = &cobra.Command{
	Use:   "obscure password",
	Short: `Obscure password for use in the rclone.conf`,
	Run: func(command *cobra.Command, args []string) {
		cmd.CheckArgs(1, 1, command, args)
		cmd.Run(false, false, command, func() error {
			obscure := fs.MustObscure(args[0])
			fmt.Println(obscure)
			return nil
		})
	},
}
