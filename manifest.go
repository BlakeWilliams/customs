package manifest

import (
	"fmt"
	"io"

	"github.com/bluekeyes/go-gitdiff/gitdiff"
)

// Import is the struct passed to plugins that provide the info necessary to
// apply rules. It includes information about the PR if present, and the diff.
type Import struct {
	// PullTitle is the title of the pull request if present.
	PullTitle string `json:"pullTitle,omitempty"`
	// PullDescription is the description of the pull request, if present
	PullDescription string `json:"pullDescription,omitempty"`
	// RepoRef is the pull request number being inspected
	PullNumber int `json:"pullNumber,omitempty"`

	// RepoOwner is the owner of the repo
	RepoOwner string `json:"repoOwner"`
	// RepoName is the name of the repo
	RepoName string `json:"repoName"`

	// CurrentSha is the sha of the current commit
	CurrentSha string `json:"currentSha"`

	// Strict is true if the inspection is running in strict mode, which means
	// it should fail if PR information is not provided.
	Strict bool `json:"strict"`

	// Diff is the parsed changes for this diff
	Diff Diff `json:"diff"`
}

// Diff represents the provided diff
type Diff struct {
	// ChangedFiles is a list of files that have been changed. It does not
	// include deleted, renamed, or new files.
	ChangedFiles []string `json:"changed"`
	// DeletedFiles is a list of files that have been deleted.
	DeletedFiles []string `json:"deleted"`
	// RenamedFiles is a list of files that have been renamed.
	RenamedFiles []string `json:"renamed"`
	// NewFiles is a list of files that have been added.
	NewFiles []string `json:"new"`
	// CopiedFiles is a list of files that have been copied.
	CopiedFiles []string `json:"copied"`

	// Files is a mapping of file names to the file contents
	Files map[string]File `json:"files"`
}

type DiffOperation string

const (
	DiffOperationNew    DiffOperation = "new"
	DiffOperationDelete DiffOperation = "delete"
	DiffOperationRename DiffOperation = "rename"
	DiffOperationChange DiffOperation = "change"
	DiffOperationCopy   DiffOperation = "copy"
)

// File is represents a single file in a diff
type File struct {
	Operation DiffOperation `json:"operation"`

	// Name is the new name of the file
	Name string `json:"new_name"`
	// OldName is the old name of the file, if it was renamed
	OldName string `json:"old_name"`

	Left  []Line `json:"left"`
	Right []Line `json:"right"`

	// TODO include mode changes
}

// Line represents a change (add/delete) in a diff
type Line struct {
	LineNo  uint   `json:"lineno"`
	Content string `json:"content"`
}

// NewDiff returns a new diff that can be used by plugins
func NewDiff(f io.Reader) (Diff, error) {
	files, _, err := gitdiff.Parse(f)

	if err != nil {
		return Diff{}, fmt.Errorf("failed to parse git diff: %w", err)
	}

	diff := &Diff{
		ChangedFiles: make([]string, 0),
		DeletedFiles: make([]string, 0),
		RenamedFiles: make([]string, 0),
		CopiedFiles:  make([]string, 0),
		NewFiles:     make([]string, 0),
		Files:        make(map[string]File, len(files)),
	}

	for _, file := range files {
		leftLines := make([]Line, 0)
		rightLines := make([]Line, 0)

		for _, fragment := range file.TextFragments {
			leftStart := fragment.OldPosition
			rightStart := fragment.NewPosition

			for _, line := range fragment.Lines {
				switch line.Op {
				case gitdiff.OpDelete:
					leftLines = append(leftLines, Line{
						LineNo:  uint(leftStart),
						Content: line.Line,
					})
					leftStart++
				case gitdiff.OpAdd:
					rightLines = append(rightLines, Line{
						LineNo:  uint(rightStart),
						Content: line.Line,
					})
					rightStart++
				default:
					leftStart++
					rightStart++
				}
			}
		}

		// Add the file to the mapping
		name := file.OldName
		if name == "" {
			name = file.NewName
		}
		diff.Files[name] = File{
			Name:      file.NewName,
			OldName:   file.OldName,
			Operation: operationForFile(file),
			Left:      leftLines,
			Right:     rightLines,
		}

		if file.IsNew {
			diff.NewFiles = append(diff.NewFiles, file.NewName)
		} else if file.IsDelete {
			diff.DeletedFiles = append(diff.DeletedFiles, file.OldName)
		} else if file.IsRename {
			diff.RenamedFiles = append(diff.RenamedFiles, file.OldName)
		} else if file.IsCopy {
			diff.CopiedFiles = append(diff.CopiedFiles, file.OldName)
		} else {
			diff.ChangedFiles = append(diff.ChangedFiles, file.OldName)
		}

	}

	return *diff, nil
}

func operationForFile(f *gitdiff.File) DiffOperation {
	if f.IsNew {
		return DiffOperationNew
	} else if f.IsDelete {
		return DiffOperationDelete
	} else if f.IsRename {
		return DiffOperationRename
	} else if f.IsCopy {
		return DiffOperationCopy
	} else {
		return DiffOperationChange
	}
}
