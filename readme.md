# fs

fs is a simple library that provides a simple abstraction over filesystem
operations that is compatible with `io/fs` from the standard library.

## Examples

**File Uploads**

Assume you're building an application to upload files and store them against a
hash of their content but with a size limit, then we could do the following with
this library,

    import (
        "crypto/sha256"
        "io"
        "net/http"

        "github.com/andrewpillar/fs"
    )

    var defaultMaxMemory int64 = 32 << 20

    func Upload(w http.ResonseWriter, r *http.Request) {
        if err := r.ParseMultipartForm(defaultMaxMemory); err != nil {
            // handle error
        }

        hdrs, ok := r.MultipartForm.File["upload"]

        if !ok {
            // handle error
        }

        tmp, err := fs.ReadFile("upload", hds[0].Open())

        if err != nil {
            // handle error
        }

        // Delete the file if it get's stored on disk from ReadFile.
        defer fs.Cleanup(tmp)

        osfs := fs.New("/tmp")
        hashfs := fs.Hash(osfs, sha256.New)
        limitfs := fs.Limit(hashfs, 5 << 20)

        f, err = limitfs.Put(tmp)

        if err != nil {
            // handle error
        }

        info, err := f.Stat()

        if err != nil {
            // handle error
        }

        w.WriteHeader(http.StatusNoContent)
        io.WriteString(info.Name())
    }

With the above example we use the library to create an FS for the operating
system via `fs.New`. This is then wrapped with `fs.Hash` to store the file
against the SHA256 hash of the file's content. This is then wrapped with
`fs.Limit` to limit the size of files that can be stored in the filesystem.
Finally the name of the uploaded file, which would be the file content's hash,
is sent in the response.
