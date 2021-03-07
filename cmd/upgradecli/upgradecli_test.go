package upgradecli

import (
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"syscall"
	"testing"

	goversion "github.com/hashicorp/go-version"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
)

func TestDownloadKelda(t *testing.T) {
	text := "advanced kubernetes file sync tool\n"
	archive := "H4sIAGYhMV0AA+3QywrCMBCF4a59inmESZrW54ltAtqQQi+Cb28v4Ep0VUT4v80h" +
		"zCzOpAup9cWxVLV2TtY819WWavf3zpViyso4a7UqnagxtbGF6MG9NvM4+WGpcvvyC8tajB/" +
		"m+yXyyj/h27vPTWilmy9hyGEKo8RrCjI+ciNT36fTrysCAAAAAAAAAAAAAAAAAN54ApUvtKYAKAAA"

	version, err := goversion.NewVersion("0.10.0")
	assert.NoError(t, err)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		query := r.URL.Query()

		w.Header().Set("Content-Type", "application/x-gzip")

		assert.Equal(t, osToParam[runtime.GOOS], query.Get("os"))
		assert.Equal(t, version.String(), query.Get("release"))
		assert.Equal(t, Token, query.Get("token"))

		file, err := base64.StdEncoding.DecodeString(archive)
		assert.NoError(t, err)

		_, err = w.Write(file)
		assert.NoError(t, err)
	}))
	defer ts.Close()

	endpoint = ts.URL
	fs = afero.NewMemMapFs()
	err = downloadKelda(version)
	assert.NoError(t, err)

	path, err := os.Getwd()
	assert.NoError(t, err)
	contents, err := afero.ReadFile(fs, filepath.Join(path, "kelda"))
	assert.NoError(t, err)
	assert.Equal(t, []byte(text), contents)
}

func TestIsWritable(t *testing.T) {
	tests := []struct {
		name   string
		mode   os.FileMode
		stat   *syscall.Stat_t
		uid    int
		gids   []int
		expRes bool
	}{
		{
			name: "User owns file and can write",
			mode: os.FileMode(0744),
			stat: &syscall.Stat_t{
				Uid: 1,
				Gid: 5,
			},
			uid:    1,
			gids:   []int{10},
			expRes: true,
		},
		{
			name: "User in group that owns file and can write",
			mode: os.FileMode(0575),
			stat: &syscall.Stat_t{
				Uid: 1,
				Gid: 10,
			},
			uid:    2,
			gids:   []int{10, 20},
			expRes: true,
		},
		{
			name: "Others can write",
			mode: os.FileMode(0557),
			stat: &syscall.Stat_t{
				Uid: 15,
				Gid: 10,
			},
			uid:    5,
			gids:   []int{20},
			expRes: true,
		},
		{
			name: "User owns but cannot write",
			mode: os.FileMode(0577),
			stat: &syscall.Stat_t{
				Uid: 5,
				Gid: 10,
			},
			uid:    5,
			gids:   []int{10},
			expRes: false,
		},
		{
			name: "Group can write but user not in group",
			mode: os.FileMode(0575),
			stat: &syscall.Stat_t{
				Uid: 5,
				Gid: 10,
			},
			uid:    20,
			gids:   []int{15},
			expRes: false,
		},
		{
			name: "Others can write but user owns file",
			mode: os.FileMode(0557),
			stat: &syscall.Stat_t{
				Uid: 5,
				Gid: 15,
			},
			uid:    5,
			gids:   []int{10},
			expRes: false,
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			res := isWritable(test.mode, test.stat, test.uid, test.gids)
			assert.Equal(t, test.expRes, res)
		})
	}
}
