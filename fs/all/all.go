package all

import (
	// Active file systems
	_ "github.com/rmdashrf/rclone_acd_hack/amazonclouddrive"
	_ "github.com/rmdashrf/rclone_acd_hack/b2"
	_ "github.com/rmdashrf/rclone_acd_hack/crypt"
	_ "github.com/rmdashrf/rclone_acd_hack/drive"
	_ "github.com/rmdashrf/rclone_acd_hack/dropbox"
	_ "github.com/rmdashrf/rclone_acd_hack/ftp"
	_ "github.com/rmdashrf/rclone_acd_hack/googlecloudstorage"
	_ "github.com/rmdashrf/rclone_acd_hack/hubic"
	_ "github.com/rmdashrf/rclone_acd_hack/local"
	_ "github.com/rmdashrf/rclone_acd_hack/onedrive"
	_ "github.com/rmdashrf/rclone_acd_hack/s3"
	_ "github.com/rmdashrf/rclone_acd_hack/sftp"
	_ "github.com/rmdashrf/rclone_acd_hack/swift"
	_ "github.com/rmdashrf/rclone_acd_hack/yandex"
)
