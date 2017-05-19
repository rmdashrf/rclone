package crypt_test

import (
	"os"
	"path/filepath"

	"github.com/rmdashrf/rclone_acd_hack/fs"
	"github.com/rmdashrf/rclone_acd_hack/fstest/fstests"
)

// Create the TestCrypt: remote
func init() {
	tempdir := filepath.Join(os.TempDir(), "rclone-crypt-test-standard")
	name := "TestCrypt"
	tempdir2 := filepath.Join(os.TempDir(), "rclone-crypt-test-off")
	name2 := name + "2"
	tempdir3 := filepath.Join(os.TempDir(), "rclone-crypt-test-obfuscate")
	name3 := name + "3"
	fstests.ExtraConfig = []fstests.ExtraConfigItem{
		{Name: name, Key: "type", Value: "crypt"},
		{Name: name, Key: "remote", Value: tempdir},
		{Name: name, Key: "password", Value: fs.MustObscure("potato")},
		{Name: name, Key: "filename_encryption", Value: "standard"},
		{Name: name2, Key: "type", Value: "crypt"},
		{Name: name2, Key: "remote", Value: tempdir2},
		{Name: name2, Key: "password", Value: fs.MustObscure("potato2")},
		{Name: name2, Key: "filename_encryption", Value: "off"},
		{Name: name3, Key: "type", Value: "crypt"},
		{Name: name3, Key: "remote", Value: tempdir3},
		{Name: name3, Key: "password", Value: fs.MustObscure("potato2")},
		{Name: name3, Key: "filename_encryption", Value: "obfuscate"},
	}
}
