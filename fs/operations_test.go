// Integration tests - test rclone by doing real transactions to a
// storage provider to and from the local disk.
//
// By default it will use a local fs, however you can provide a
// -remote option to use a different remote.  The test_all.go script
// is a wrapper to call this for all the test remotes.
//
// FIXME not safe for concurrent running of tests until fs.Config is
// no longer a global
//
// NB When writing tests
//
// Make sure every series of writes to the remote has a
// fstest.CheckItems() before use.  This make sure the directory
// listing is now consistent and stops cascading errors.
//
// Call fs.Stats.ResetCounters() before every fs.Sync() as it uses the
// error count internally.

package fs_test

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/rmdashrf/rclone_acd_hack/fs"
	_ "github.com/rmdashrf/rclone_acd_hack/fs/all" // import all fs
	"github.com/rmdashrf/rclone_acd_hack/fstest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Globals
var (
	RemoteName      = flag.String("remote", "", "Remote to test with, defaults to local filesystem")
	SubDir          = flag.Bool("subdir", false, "Set to test with a sub directory")
	Verbose         = flag.Bool("verbose", false, "Set to enable logging")
	DumpHeaders     = flag.Bool("dump-headers", false, "Set to dump headers (needs -verbose)")
	DumpBodies      = flag.Bool("dump-bodies", false, "Set to dump bodies (needs -verbose)")
	Individual      = flag.Bool("individual", false, "Make individual bucket/container/directory for each test - much slower")
	LowLevelRetries = flag.Int("low-level-retries", 10, "Number of low level retries")
)

// Some times used in the tests
var (
	t1 = fstest.Time("2001-02-03T04:05:06.499999999Z")
	t2 = fstest.Time("2011-12-25T12:59:59.123456789Z")
	t3 = fstest.Time("2011-12-30T12:59:59.000000000Z")
)

// TestMain drives the tests
func TestMain(m *testing.M) {
	flag.Parse()
	if !*Individual {
		oneRun = newRun()
	}
	rc := m.Run()
	if !*Individual {
		oneRun.Finalise()
	}
	os.Exit(rc)
}

// Run holds the remotes for a test run
type Run struct {
	localName    string
	flocal       fs.Fs
	fremote      fs.Fs
	fremoteName  string
	cleanRemote  func()
	mkdir        map[string]bool // whether the remote has been made yet for the fs name
	Logf, Fatalf func(text string, args ...interface{})
}

// oneRun holds the master run data if individual is not set
var oneRun *Run

// newRun initialise the remote and local for testing and returns a
// run object.
//
// r.flocal is an empty local Fs
// r.fremote is an empty remote Fs
//
// Finalise() will tidy them away when done.
func newRun() *Run {
	r := &Run{
		Logf:   log.Printf,
		Fatalf: log.Fatalf,
		mkdir:  make(map[string]bool),
	}

	// Never ask for passwords, fail instead.
	// If your local config is encrypted set environment variable
	// "RCLONE_CONFIG_PASS=hunter2" (or your password)
	*fs.AskPassword = false
	fs.LoadConfig()
	if *Verbose {
		fs.Config.LogLevel = fs.LogLevelDebug
	}
	fs.Config.DumpHeaders = *DumpHeaders
	fs.Config.DumpBodies = *DumpBodies
	fs.Config.LowLevelRetries = *LowLevelRetries
	var err error
	r.fremote, r.fremoteName, r.cleanRemote, err = fstest.RandomRemote(*RemoteName, *SubDir)
	if err != nil {
		r.Fatalf("Failed to open remote %q: %v", *RemoteName, err)
	}

	r.localName, err = ioutil.TempDir("", "rclone")
	if err != nil {
		r.Fatalf("Failed to create temp dir: %v", err)
	}
	r.localName = filepath.ToSlash(r.localName)
	r.flocal, err = fs.NewFs(r.localName)
	if err != nil {
		r.Fatalf("Failed to make %q: %v", r.localName, err)
	}
	fs.CalculateModifyWindow(r.fremote, r.flocal)
	return r
}

// dirsToRemove sorts by string length
type dirsToRemove []string

func (d dirsToRemove) Len() int           { return len(d) }
func (d dirsToRemove) Swap(i, j int)      { d[i], d[j] = d[j], d[i] }
func (d dirsToRemove) Less(i, j int) bool { return len(d[i]) > len(d[j]) }

// NewRun initialise the remote and local for testing and returns a
// run object.  Call this from the tests.
//
// r.flocal is an empty local Fs
// r.fremote is an empty remote Fs
//
// Finalise() will tidy them away when done.
func NewRun(t *testing.T) *Run {
	var r *Run
	if *Individual {
		r = newRun()
	} else {
		// If not individual, use the global one with the clean method overridden
		r = new(Run)
		*r = *oneRun
		r.cleanRemote = func() {
			var toDelete dirsToRemove
			list := fs.NewLister().Start(r.fremote, "")
			for {
				o, dir, err := list.Get()
				if err != nil {
					if err == fs.ErrorDirNotFound {
						break
					}
					t.Fatalf("Error listing: %v", err)
				} else if o != nil {
					err = o.Remove()
					if err != nil {
						t.Errorf("Error removing file %q: %v", o.Remote(), err)
					}
				} else if dir != nil {
					toDelete = append(toDelete, dir.Remote())
				} else {
					break
				}
			}
			sort.Sort(toDelete)
			for _, dir := range toDelete {
				err := r.fremote.Rmdir(dir)
				if err != nil {
					t.Errorf("Error removing dir %q: %v", dir, err)
				}
			}
			// Check remote is empty
			fstest.CheckItems(t, r.fremote)
		}
	}
	r.Logf = t.Logf
	r.Fatalf = t.Fatalf
	r.Logf("Remote %q, Local %q, Modify Window %q", r.fremote, r.flocal, fs.Config.ModifyWindow)
	return r
}

// Rename a file in local
func (r *Run) RenameFile(item fstest.Item, newpath string) fstest.Item {
	oldFilepath := path.Join(r.localName, item.Path)
	newFilepath := path.Join(r.localName, newpath)
	if err := os.Rename(oldFilepath, newFilepath); err != nil {
		r.Fatalf("Failed to rename file from %q to %q: %v", item.Path, newpath, err)
	}

	item.Path = newpath

	return item
}

// Write a file to local
func (r *Run) WriteFile(filePath, content string, t time.Time) fstest.Item {
	item := fstest.NewItem(filePath, content, t)
	// FIXME make directories?
	filePath = path.Join(r.localName, filePath)
	dirPath := path.Dir(filePath)
	err := os.MkdirAll(dirPath, 0770)
	if err != nil {
		r.Fatalf("Failed to make directories %q: %v", dirPath, err)
	}
	err = ioutil.WriteFile(filePath, []byte(content), 0600)
	if err != nil {
		r.Fatalf("Failed to write file %q: %v", filePath, err)
	}
	err = os.Chtimes(filePath, t, t)
	if err != nil {
		r.Fatalf("Failed to chtimes file %q: %v", filePath, err)
	}
	return item
}

// ForceMkdir creates the remote
func (r *Run) ForceMkdir(f fs.Fs) {
	err := f.Mkdir("")
	if err != nil {
		r.Fatalf("Failed to mkdir %q: %v", f, err)
	}
	r.mkdir[f.String()] = true
}

// Mkdir creates the remote if it hasn't been created already
func (r *Run) Mkdir(f fs.Fs) {
	if !r.mkdir[f.String()] {
		r.ForceMkdir(f)
	}
}

// WriteObjectTo writes an object to the fs, remote passed in
func (r *Run) WriteObjectTo(f fs.Fs, remote, content string, modTime time.Time, useUnchecked bool) fstest.Item {
	put := f.Put
	if useUnchecked {
		put = f.Features().PutUnchecked
		if put == nil {
			r.Fatalf("Fs doesn't support PutUnchecked")
		}
	}
	r.Mkdir(f)
	const maxTries = 10
	for tries := 1; ; tries++ {
		in := bytes.NewBufferString(content)
		objinfo := fs.NewStaticObjectInfo(remote, modTime, int64(len(content)), true, nil, nil)
		_, err := put(in, objinfo)
		if err == nil {
			break
		}
		// Retry if err returned a retry error
		if fs.IsRetryError(err) && tries < maxTries {
			r.Logf("Retry Put of %q to %v: %d/%d (%v)", remote, f, tries, maxTries, err)
			time.Sleep(2 * time.Second)
			continue
		}
		r.Fatalf("Failed to put %q to %q: %v", remote, f, err)
	}
	return fstest.NewItem(remote, content, modTime)
}

// WriteObject writes an object to the remote
func (r *Run) WriteObject(remote, content string, modTime time.Time) fstest.Item {
	return r.WriteObjectTo(r.fremote, remote, content, modTime, false)
}

// WriteUncheckedObject writes an object to the remote not checking for duplicates
func (r *Run) WriteUncheckedObject(remote, content string, modTime time.Time) fstest.Item {
	return r.WriteObjectTo(r.fremote, remote, content, modTime, true)
}

// WriteBoth calls WriteObject and WriteFile with the same arguments
func (r *Run) WriteBoth(remote, content string, modTime time.Time) fstest.Item {
	r.WriteFile(remote, content, modTime)
	return r.WriteObject(remote, content, modTime)
}

// Clean the temporary directory
func (r *Run) cleanTempDir() {
	err := os.RemoveAll(r.localName)
	if err != nil {
		r.Logf("Failed to clean temporary directory %q: %v", r.localName, err)
	}
}

// finalise cleans the remote and local
func (r *Run) Finalise() {
	// r.Logf("Cleaning remote %q", r.fremote)
	r.cleanRemote()
	// r.Logf("Cleaning local %q", r.localName)
	r.cleanTempDir()
}

// ------------------------------------------------------------

func TestMkdir(t *testing.T) {
	r := NewRun(t)
	defer r.Finalise()
	fstest.TestMkdir(t, r.fremote)
}

func TestLsd(t *testing.T) {
	r := NewRun(t)
	defer r.Finalise()
	file1 := r.WriteObject("sub dir/hello world", "hello world", t1)

	fstest.CheckItems(t, r.fremote, file1)

	var buf bytes.Buffer
	err := fs.ListDir(r.fremote, &buf)
	require.NoError(t, err)
	res := buf.String()
	assert.Contains(t, res, "sub dir\n")
}

func TestLs(t *testing.T) {
	r := NewRun(t)
	defer r.Finalise()
	file1 := r.WriteBoth("potato2", "------------------------------------------------------------", t1)
	file2 := r.WriteBoth("empty space", "", t2)

	fstest.CheckItems(t, r.fremote, file1, file2)

	var buf bytes.Buffer
	err := fs.List(r.fremote, &buf)
	require.NoError(t, err)
	res := buf.String()
	assert.Contains(t, res, "        0 empty space\n")
	assert.Contains(t, res, "       60 potato2\n")
}

func TestLsLong(t *testing.T) {
	r := NewRun(t)
	defer r.Finalise()
	file1 := r.WriteBoth("potato2", "------------------------------------------------------------", t1)
	file2 := r.WriteBoth("empty space", "", t2)

	fstest.CheckItems(t, r.fremote, file1, file2)

	var buf bytes.Buffer
	err := fs.ListLong(r.fremote, &buf)
	require.NoError(t, err)
	res := buf.String()
	lines := strings.Split(strings.Trim(res, "\n"), "\n")
	assert.Equal(t, 2, len(lines))

	timeFormat := "2006-01-02 15:04:05.000000000"
	precision := r.fremote.Precision()
	location := time.Now().Location()
	checkTime := func(m, filename string, expected time.Time) {
		modTime, err := time.ParseInLocation(timeFormat, m, location) // parse as localtime
		if err != nil {
			t.Errorf("Error parsing %q: %v", m, err)
		} else {
			dt, ok := fstest.CheckTimeEqualWithPrecision(expected, modTime, precision)
			if !ok {
				t.Errorf("%s: Modification time difference too big |%s| > %s (%s vs %s) (precision %s)", filename, dt, precision, modTime, expected, precision)
			}
		}
	}

	m1 := regexp.MustCompile(`(?m)^        0 (\d{4}-\d\d-\d\d \d\d:\d\d:\d\d\.\d{9}) empty space$`)
	if ms := m1.FindStringSubmatch(res); ms == nil {
		t.Errorf("empty space missing: %q", res)
	} else {
		checkTime(ms[1], "empty space", t2.Local())
	}

	m2 := regexp.MustCompile(`(?m)^       60 (\d{4}-\d\d-\d\d \d\d:\d\d:\d\d\.\d{9}) potato2$`)
	if ms := m2.FindStringSubmatch(res); ms == nil {
		t.Errorf("potato2 missing: %q", res)
	} else {
		checkTime(ms[1], "potato2", t1.Local())
	}
}

func TestMd5sum(t *testing.T) {
	r := NewRun(t)
	defer r.Finalise()
	file1 := r.WriteBoth("potato2", "------------------------------------------------------------", t1)
	file2 := r.WriteBoth("empty space", "", t2)

	fstest.CheckItems(t, r.fremote, file1, file2)

	var buf bytes.Buffer
	err := fs.Md5sum(r.fremote, &buf)
	require.NoError(t, err)
	res := buf.String()
	if !strings.Contains(res, "d41d8cd98f00b204e9800998ecf8427e  empty space\n") &&
		!strings.Contains(res, "                     UNSUPPORTED  empty space\n") &&
		!strings.Contains(res, "                                  empty space\n") {
		t.Errorf("empty space missing: %q", res)
	}
	if !strings.Contains(res, "6548b156ea68a4e003e786df99eee76  potato2\n") &&
		!strings.Contains(res, "                     UNSUPPORTED  potato2\n") &&
		!strings.Contains(res, "                                  potato2\n") {
		t.Errorf("potato2 missing: %q", res)
	}
}

func TestSha1sum(t *testing.T) {
	r := NewRun(t)
	defer r.Finalise()
	file1 := r.WriteBoth("potato2", "------------------------------------------------------------", t1)
	file2 := r.WriteBoth("empty space", "", t2)

	fstest.CheckItems(t, r.fremote, file1, file2)

	var buf bytes.Buffer
	err := fs.Sha1sum(r.fremote, &buf)
	require.NoError(t, err)
	res := buf.String()
	if !strings.Contains(res, "da39a3ee5e6b4b0d3255bfef95601890afd80709  empty space\n") &&
		!strings.Contains(res, "                             UNSUPPORTED  empty space\n") &&
		!strings.Contains(res, "                                          empty space\n") {
		t.Errorf("empty space missing: %q", res)
	}
	if !strings.Contains(res, "9dc7f7d3279715991a22853f5981df582b7f9f6d  potato2\n") &&
		!strings.Contains(res, "                             UNSUPPORTED  potato2\n") &&
		!strings.Contains(res, "                                          potato2\n") {
		t.Errorf("potato2 missing: %q", res)
	}
}

func TestCount(t *testing.T) {
	r := NewRun(t)
	defer r.Finalise()
	file1 := r.WriteBoth("potato2", "------------------------------------------------------------", t1)
	file2 := r.WriteBoth("empty space", "", t2)
	file3 := r.WriteBoth("sub dir/potato3", "hello", t2)

	fstest.CheckItems(t, r.fremote, file1, file2, file3)

	// Check the MaxDepth too
	fs.Config.MaxDepth = 1
	defer func() { fs.Config.MaxDepth = -1 }()

	objects, size, err := fs.Count(r.fremote)
	require.NoError(t, err)
	assert.Equal(t, int64(2), objects)
	assert.Equal(t, int64(60), size)
}

func TestDelete(t *testing.T) {
	r := NewRun(t)
	defer r.Finalise()
	file1 := r.WriteObject("small", "1234567890", t2)                                                                                           // 10 bytes
	file2 := r.WriteObject("medium", "------------------------------------------------------------", t1)                                        // 60 bytes
	file3 := r.WriteObject("large", "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA", t1) // 100 bytes
	fstest.CheckItems(t, r.fremote, file1, file2, file3)

	fs.Config.Filter.MaxSize = 60
	defer func() {
		fs.Config.Filter.MaxSize = -1
	}()

	err := fs.Delete(r.fremote)
	require.NoError(t, err)
	fstest.CheckItems(t, r.fremote, file3)
}

func testCheck(t *testing.T, checkFunction func(fdst, fsrc fs.Fs) error) {
	r := NewRun(t)
	defer r.Finalise()

	check := func(i int, wantErrors int64) {
		fs.Debugf(r.fremote, "%d: Starting check test", i)
		oldErrors := fs.Stats.GetErrors()
		err := checkFunction(r.flocal, r.fremote)
		gotErrors := fs.Stats.GetErrors() - oldErrors
		if wantErrors == 0 && err != nil {
			t.Errorf("%d: Got error when not expecting one: %v", i, err)
		}
		if wantErrors != 0 && err == nil {
			t.Errorf("%d: No error when expecting one", i)
		}
		if wantErrors != gotErrors {
			t.Errorf("%d: Expecting %d errors but got %d", i, wantErrors, gotErrors)
		}
		fs.Debugf(r.fremote, "%d: Ending check test", i)
	}

	file1 := r.WriteBoth("rutabaga", "is tasty", t3)
	fstest.CheckItems(t, r.fremote, file1)
	fstest.CheckItems(t, r.flocal, file1)
	check(1, 0)

	file2 := r.WriteFile("potato2", "------------------------------------------------------------", t1)
	fstest.CheckItems(t, r.flocal, file1, file2)
	check(2, 1)

	file3 := r.WriteObject("empty space", "", t2)
	fstest.CheckItems(t, r.fremote, file1, file3)
	check(3, 2)

	file2r := file2
	if fs.Config.SizeOnly {
		file2r = r.WriteObject("potato2", "--Some-Differences-But-Size-Only-Is-Enabled-----------------", t1)
	} else {
		r.WriteObject("potato2", "------------------------------------------------------------", t1)
	}
	fstest.CheckItems(t, r.fremote, file1, file2r, file3)
	check(4, 1)

	r.WriteFile("empty space", "", t2)
	fstest.CheckItems(t, r.flocal, file1, file2, file3)
	check(5, 0)
}

func TestCheck(t *testing.T) {
	testCheck(t, fs.Check)
}

func TestCheckDownload(t *testing.T) {
	testCheck(t, fs.CheckDownload)
}

func TestCheckSizeOnly(t *testing.T) {
	fs.Config.SizeOnly = true
	defer func() { fs.Config.SizeOnly = false }()
	TestCheck(t)
}

func (r *Run) checkWithDuplicates(t *testing.T, items ...fstest.Item) {
	objects, size, err := fs.Count(r.fremote)
	require.NoError(t, err)
	assert.Equal(t, int64(len(items)), objects)
	wantSize := int64(0)
	for _, item := range items {
		wantSize += item.Size
	}
	assert.Equal(t, wantSize, size)
}

func skipIfCantDedupe(t *testing.T, f fs.Fs) {
	if f.Features().PutUnchecked == nil {
		t.Skip("Can't test deduplicate - no PutUnchecked")
	}
	if !f.Features().DuplicateFiles {
		t.Skip("Can't test deduplicate - no duplicate files possible")
	}
	if !f.Hashes().Contains(fs.HashMD5) {
		t.Skip("Can't test deduplicate - MD5 not supported")
	}
}

func TestDeduplicateInteractive(t *testing.T) {
	r := NewRun(t)
	defer r.Finalise()
	skipIfCantDedupe(t, r.fremote)

	file1 := r.WriteUncheckedObject("one", "This is one", t1)
	file2 := r.WriteUncheckedObject("one", "This is one", t1)
	file3 := r.WriteUncheckedObject("one", "This is one", t1)
	r.checkWithDuplicates(t, file1, file2, file3)

	err := fs.Deduplicate(r.fremote, fs.DeduplicateInteractive)
	require.NoError(t, err)

	fstest.CheckItems(t, r.fremote, file1)
}

func TestDeduplicateSkip(t *testing.T) {
	r := NewRun(t)
	defer r.Finalise()
	skipIfCantDedupe(t, r.fremote)

	file1 := r.WriteUncheckedObject("one", "This is one", t1)
	file2 := r.WriteUncheckedObject("one", "This is one", t1)
	file3 := r.WriteUncheckedObject("one", "This is another one", t1)
	r.checkWithDuplicates(t, file1, file2, file3)

	err := fs.Deduplicate(r.fremote, fs.DeduplicateSkip)
	require.NoError(t, err)

	r.checkWithDuplicates(t, file1, file3)
}

func TestDeduplicateFirst(t *testing.T) {
	r := NewRun(t)
	defer r.Finalise()
	skipIfCantDedupe(t, r.fremote)

	file1 := r.WriteUncheckedObject("one", "This is one", t1)
	file2 := r.WriteUncheckedObject("one", "This is one A", t1)
	file3 := r.WriteUncheckedObject("one", "This is one BB", t1)
	r.checkWithDuplicates(t, file1, file2, file3)

	err := fs.Deduplicate(r.fremote, fs.DeduplicateFirst)
	require.NoError(t, err)

	objects, size, err := fs.Count(r.fremote)
	require.NoError(t, err)
	assert.Equal(t, int64(1), objects)
	if size != file1.Size && size != file2.Size && size != file3.Size {
		t.Errorf("Size not one of the object sizes %d", size)
	}
}

func TestDeduplicateNewest(t *testing.T) {
	r := NewRun(t)
	defer r.Finalise()
	skipIfCantDedupe(t, r.fremote)

	file1 := r.WriteUncheckedObject("one", "This is one", t1)
	file2 := r.WriteUncheckedObject("one", "This is one too", t2)
	file3 := r.WriteUncheckedObject("one", "This is another one", t3)
	r.checkWithDuplicates(t, file1, file2, file3)

	err := fs.Deduplicate(r.fremote, fs.DeduplicateNewest)
	require.NoError(t, err)

	fstest.CheckItems(t, r.fremote, file3)
}

func TestDeduplicateOldest(t *testing.T) {
	r := NewRun(t)
	defer r.Finalise()
	skipIfCantDedupe(t, r.fremote)

	file1 := r.WriteUncheckedObject("one", "This is one", t1)
	file2 := r.WriteUncheckedObject("one", "This is one too", t2)
	file3 := r.WriteUncheckedObject("one", "This is another one", t3)
	r.checkWithDuplicates(t, file1, file2, file3)

	err := fs.Deduplicate(r.fremote, fs.DeduplicateOldest)
	require.NoError(t, err)

	fstest.CheckItems(t, r.fremote, file1)
}

func TestDeduplicateRename(t *testing.T) {
	r := NewRun(t)
	defer r.Finalise()
	skipIfCantDedupe(t, r.fremote)

	file1 := r.WriteUncheckedObject("one.txt", "This is one", t1)
	file2 := r.WriteUncheckedObject("one.txt", "This is one too", t2)
	file3 := r.WriteUncheckedObject("one.txt", "This is another one", t3)
	r.checkWithDuplicates(t, file1, file2, file3)

	err := fs.Deduplicate(r.fremote, fs.DeduplicateRename)
	require.NoError(t, err)

	list := fs.NewLister().Start(r.fremote, "")
	for {
		o, err := list.GetObject()
		require.NoError(t, err)
		// Check if we are finished
		if o == nil {
			break
		}
		remote := o.Remote()
		if remote != "one-1.txt" &&
			remote != "one-2.txt" &&
			remote != "one-3.txt" {
			t.Errorf("Bad file name after rename %q", remote)
		}
		size := o.Size()
		if size != file1.Size && size != file2.Size && size != file3.Size {
			t.Errorf("Size not one of the object sizes %d", size)
		}
	}
}

func TestCat(t *testing.T) {
	r := NewRun(t)
	defer r.Finalise()
	file1 := r.WriteBoth("file1", "ABCDEFGHIJ", t1)
	file2 := r.WriteBoth("file2", "012345678", t2)

	fstest.CheckItems(t, r.fremote, file1, file2)

	for _, test := range []struct {
		offset int64
		count  int64
		a      string
		b      string
	}{
		{0, -1, "ABCDEFGHIJ", "012345678"},
		{0, 5, "ABCDE", "01234"},
		{-3, -1, "HIJ", "678"},
		{2, 3, "CDE", "234"},
	} {
		var buf bytes.Buffer
		err := fs.Cat(r.fremote, &buf, test.offset, test.count)
		require.NoError(t, err)
		res := buf.String()

		if res != test.a+test.b && res != test.b+test.a {
			t.Errorf("Incorrect output from Cat(%d,%d): %q", test.offset, test.count, res)
		}
	}
}

func TestRmdirs(t *testing.T) {
	r := NewRun(t)
	defer r.Finalise()
	r.Mkdir(r.fremote)

	// Clean any directories that have crept in so far
	// FIXME make the Finalise method do this?
	require.NoError(t, fs.Rmdirs(r.fremote, ""))

	// Make some files and dirs we expect to keep
	r.ForceMkdir(r.fremote)
	file1 := r.WriteObject("A1/B1/C1/one", "aaa", t1)
	file2 := r.WriteObject("A1/two", "bbb", t2)
	//..and dirs we expect to delete
	require.NoError(t, fs.Mkdir(r.fremote, "A2"))
	require.NoError(t, fs.Mkdir(r.fremote, "A1/B2"))
	require.NoError(t, fs.Mkdir(r.fremote, "A1/B2/C2"))
	require.NoError(t, fs.Mkdir(r.fremote, "A1/B1/C3"))
	require.NoError(t, fs.Mkdir(r.fremote, "A3"))
	require.NoError(t, fs.Mkdir(r.fremote, "A3/B3"))
	require.NoError(t, fs.Mkdir(r.fremote, "A3/B3/C4"))

	fstest.CheckListingWithPrecision(
		t,
		r.fremote,
		[]fstest.Item{
			file1, file2,
		},
		[]string{
			"A1",
			"A1/B1",
			"A1/B1/C1",
			"A2",
			"A1/B2",
			"A1/B2/C2",
			"A1/B1/C3",
			"A3",
			"A3/B3",
			"A3/B3/C4",
		},
		fs.Config.ModifyWindow,
	)

	require.NoError(t, fs.Rmdirs(r.fremote, ""))

	fstest.CheckListingWithPrecision(
		t,
		r.fremote,
		[]fstest.Item{
			file1, file2,
		},
		[]string{
			"A1",
			"A1/B1",
			"A1/B1/C1",
		},
		fs.Config.ModifyWindow,
	)

}

func TestMoveFile(t *testing.T) {
	r := NewRun(t)
	defer r.Finalise()

	file1 := r.WriteFile("file1", "file1 contents", t1)
	fstest.CheckItems(t, r.flocal, file1)

	file2 := file1
	file2.Path = "sub/file2"

	err := fs.MoveFile(r.fremote, r.flocal, file2.Path, file1.Path)
	require.NoError(t, err)
	fstest.CheckItems(t, r.flocal)
	fstest.CheckItems(t, r.fremote, file2)

	r.WriteFile("file1", "file1 contents", t1)
	fstest.CheckItems(t, r.flocal, file1)

	err = fs.MoveFile(r.fremote, r.flocal, file2.Path, file1.Path)
	require.NoError(t, err)
	fstest.CheckItems(t, r.flocal)
	fstest.CheckItems(t, r.fremote, file2)
}

func TestCopyFile(t *testing.T) {
	r := NewRun(t)
	defer r.Finalise()

	file1 := r.WriteFile("file1", "file1 contents", t1)
	fstest.CheckItems(t, r.flocal, file1)

	file2 := file1
	file2.Path = "sub/file2"

	err := fs.CopyFile(r.fremote, r.flocal, file2.Path, file1.Path)
	require.NoError(t, err)
	fstest.CheckItems(t, r.flocal, file1)
	fstest.CheckItems(t, r.fremote, file2)

	err = fs.CopyFile(r.fremote, r.flocal, file2.Path, file1.Path)
	require.NoError(t, err)
	fstest.CheckItems(t, r.flocal, file1)
	fstest.CheckItems(t, r.fremote, file2)
}

// testFsInfo is for unit testing fs.Info
type testFsInfo struct {
	name      string
	root      string
	stringVal string
	precision time.Duration
	hashes    fs.HashSet
	features  fs.Features
}

// Name of the remote (as passed into NewFs)
func (i *testFsInfo) Name() string { return i.name }

// Root of the remote (as passed into NewFs)
func (i *testFsInfo) Root() string { return i.root }

// String returns a description of the FS
func (i *testFsInfo) String() string { return i.stringVal }

// Precision of the ModTimes in this Fs
func (i *testFsInfo) Precision() time.Duration { return i.precision }

// Returns the supported hash types of the filesystem
func (i *testFsInfo) Hashes() fs.HashSet { return i.hashes }

// Returns the supported hash types of the filesystem
func (i *testFsInfo) Features() *fs.Features { return &i.features }

func TestSameConfig(t *testing.T) {
	a := &testFsInfo{name: "name", root: "root"}
	for _, test := range []struct {
		name     string
		root     string
		expected bool
	}{
		{"name", "root", true},
		{"name", "rooty", true},
		{"namey", "root", false},
		{"namey", "roott", false},
	} {
		b := &testFsInfo{name: test.name, root: test.root}
		actual := fs.SameConfig(a, b)
		assert.Equal(t, test.expected, actual)
		actual = fs.SameConfig(b, a)
		assert.Equal(t, test.expected, actual)
	}
}

func TestSame(t *testing.T) {
	a := &testFsInfo{name: "name", root: "root"}
	for _, test := range []struct {
		name     string
		root     string
		expected bool
	}{
		{"name", "root", true},
		{"name", "rooty", false},
		{"namey", "root", false},
		{"namey", "roott", false},
	} {
		b := &testFsInfo{name: test.name, root: test.root}
		actual := fs.Same(a, b)
		assert.Equal(t, test.expected, actual)
		actual = fs.Same(b, a)
		assert.Equal(t, test.expected, actual)
	}
}

func TestOverlapping(t *testing.T) {
	a := &testFsInfo{name: "name", root: "root"}
	for _, test := range []struct {
		name     string
		root     string
		expected bool
	}{
		{"name", "root", true},
		{"namey", "root", false},
		{"name", "rooty", false},
		{"namey", "rooty", false},
		{"name", "roo", false},
		{"name", "root/toot", true},
		{"name", "root/toot/", true},
		{"name", "", true},
		{"name", "/", true},
	} {
		b := &testFsInfo{name: test.name, root: test.root}
		what := fmt.Sprintf("(%q,%q) vs (%q,%q)", a.name, a.root, b.name, b.root)
		actual := fs.Overlapping(a, b)
		assert.Equal(t, test.expected, actual, what)
		actual = fs.Overlapping(b, a)
		assert.Equal(t, test.expected, actual, what)
	}
}

func TestListDirSorted(t *testing.T) {
	r := NewRun(t)
	defer r.Finalise()

	fs.Config.Filter.MaxSize = 10
	defer func() {
		fs.Config.Filter.MaxSize = -1
	}()

	files := []fstest.Item{
		r.WriteObject("a.txt", "hello world", t1),
		r.WriteObject("zend.txt", "hello", t1),
		r.WriteObject("sub dir/hello world", "hello world", t1),
		r.WriteObject("sub dir/hello world2", "hello world", t1),
		r.WriteObject("sub dir/sub sub dir/hello world3", "hello world", t1),
	}
	fstest.CheckItems(t, r.fremote, files...)
	var items fs.DirEntries
	var err error

	// Turn the BasicInfo into a name, ending with a / if it is a
	// dir
	str := func(i int) string {
		item := items[i]
		name := item.Remote()
		switch item.(type) {
		case fs.Object:
		case *fs.Dir:
			name += "/"
		default:
			t.Fatalf("Unknown type %+v", item)
		}
		return name
	}

	items, err = fs.ListDirSorted(r.fremote, true, "")
	require.NoError(t, err)
	require.Len(t, items, 3)
	assert.Equal(t, "a.txt", str(0))
	assert.Equal(t, "sub dir/", str(1))
	assert.Equal(t, "zend.txt", str(2))

	items, err = fs.ListDirSorted(r.fremote, false, "")
	require.NoError(t, err)
	require.Len(t, items, 2)
	assert.Equal(t, "sub dir/", str(0))
	assert.Equal(t, "zend.txt", str(1))

	items, err = fs.ListDirSorted(r.fremote, true, "sub dir")
	require.NoError(t, err)
	require.Len(t, items, 3)
	assert.Equal(t, "sub dir/hello world", str(0))
	assert.Equal(t, "sub dir/hello world2", str(1))
	assert.Equal(t, "sub dir/sub sub dir/", str(2))

	items, err = fs.ListDirSorted(r.fremote, false, "sub dir")
	require.NoError(t, err)
	require.Len(t, items, 1)
	assert.Equal(t, "sub dir/sub sub dir/", str(0))
}

type byteReader struct {
	c byte
}

func (br *byteReader) Read(p []byte) (n int, err error) {
	if br.c == 0 {
		err = io.EOF
	} else if len(p) >= 1 {
		p[0] = br.c
		n = 1
		br.c--
	}
	return
}

func TestReadFill(t *testing.T) {
	buf := []byte{9, 9, 9, 9, 9}

	n, err := fs.ReadFill(&byteReader{0}, buf)
	assert.Equal(t, io.EOF, err)
	assert.Equal(t, 0, n)
	assert.Equal(t, []byte{9, 9, 9, 9, 9}, buf)

	n, err = fs.ReadFill(&byteReader{3}, buf)
	assert.Equal(t, io.EOF, err)
	assert.Equal(t, 3, n)
	assert.Equal(t, []byte{3, 2, 1, 9, 9}, buf)

	n, err = fs.ReadFill(&byteReader{8}, buf)
	assert.Equal(t, nil, err)
	assert.Equal(t, 5, n)
	assert.Equal(t, []byte{8, 7, 6, 5, 4}, buf)
}

type errorReader struct {
	err error
}

func (er errorReader) Read(p []byte) (n int, err error) {
	return 0, er.err
}

func TestCheckEqualReaders(t *testing.T) {
	b65a := make([]byte, 65*1024)
	b65b := make([]byte, 65*1024)
	b65b[len(b65b)-1] = 1
	b66 := make([]byte, 66*1024)

	differ, err := fs.CheckEqualReaders(bytes.NewBuffer(b65a), bytes.NewBuffer(b65a))
	assert.NoError(t, err)
	assert.Equal(t, differ, false)

	differ, err = fs.CheckEqualReaders(bytes.NewBuffer(b65a), bytes.NewBuffer(b65b))
	assert.NoError(t, err)
	assert.Equal(t, differ, true)

	differ, err = fs.CheckEqualReaders(bytes.NewBuffer(b65a), bytes.NewBuffer(b66))
	assert.NoError(t, err)
	assert.Equal(t, differ, true)

	differ, err = fs.CheckEqualReaders(bytes.NewBuffer(b66), bytes.NewBuffer(b65a))
	assert.NoError(t, err)
	assert.Equal(t, differ, true)

	myErr := errors.New("sentinel")
	wrap := func(b []byte) io.Reader {
		r := bytes.NewBuffer(b)
		e := errorReader{myErr}
		return io.MultiReader(r, e)
	}

	differ, err = fs.CheckEqualReaders(wrap(b65a), bytes.NewBuffer(b65a))
	assert.Equal(t, myErr, err)
	assert.Equal(t, differ, true)

	differ, err = fs.CheckEqualReaders(wrap(b65a), bytes.NewBuffer(b65b))
	assert.Equal(t, myErr, err)
	assert.Equal(t, differ, true)

	differ, err = fs.CheckEqualReaders(wrap(b65a), bytes.NewBuffer(b66))
	assert.Equal(t, myErr, err)
	assert.Equal(t, differ, true)

	differ, err = fs.CheckEqualReaders(wrap(b66), bytes.NewBuffer(b65a))
	assert.Equal(t, myErr, err)
	assert.Equal(t, differ, true)

	differ, err = fs.CheckEqualReaders(bytes.NewBuffer(b65a), wrap(b65a))
	assert.Equal(t, myErr, err)
	assert.Equal(t, differ, true)

	differ, err = fs.CheckEqualReaders(bytes.NewBuffer(b65a), wrap(b65b))
	assert.Equal(t, myErr, err)
	assert.Equal(t, differ, true)

	differ, err = fs.CheckEqualReaders(bytes.NewBuffer(b65a), wrap(b66))
	assert.Equal(t, myErr, err)
	assert.Equal(t, differ, true)

	differ, err = fs.CheckEqualReaders(bytes.NewBuffer(b66), wrap(b65a))
	assert.Equal(t, myErr, err)
	assert.Equal(t, differ, true)
}
