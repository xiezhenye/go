// Copyright 2009 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package os

import (
	"internal/syscall/windows"
	"syscall"
	"unsafe"
)

// Stat returns the FileInfo structure describing file.
// If there is an error, it will be of type *PathError.
func (file *File) Stat() (FileInfo, error) {
	if file == nil {
		return nil, ErrInvalid
	}
	if file == nil || file.pfd.Sysfd < 0 {
		return nil, syscall.EINVAL
	}
	if file.isdir() {
		// I don't know any better way to do that for directory
		return Stat(file.dirinfo.path)
	}
	if file.name == DevNull {
		return &devNullStat, nil
	}

	ft, err := file.pfd.GetFileType()
	if err != nil {
		return nil, &PathError{"GetFileType", file.name, err}
	}
	switch ft {
	case syscall.FILE_TYPE_PIPE, syscall.FILE_TYPE_CHAR:
		return &fileStat{name: basename(file.name), filetype: ft}, nil
	}

	var d syscall.ByHandleFileInformation
	err = file.pfd.GetFileInformationByHandle(&d)
	if err != nil {
		return nil, &PathError{"GetFileInformationByHandle", file.name, err}
	}
	return &fileStat{
		name: basename(file.name),
		sys: syscall.Win32FileAttributeData{
			FileAttributes: d.FileAttributes,
			CreationTime:   d.CreationTime,
			LastAccessTime: d.LastAccessTime,
			LastWriteTime:  d.LastWriteTime,
			FileSizeHigh:   d.FileSizeHigh,
			FileSizeLow:    d.FileSizeLow,
		},
		filetype: ft,
		vol:      d.VolumeSerialNumber,
		idxhi:    d.FileIndexHigh,
		idxlo:    d.FileIndexLow,
	}, nil
}

// Stat returns a FileInfo structure describing the named file.
// If there is an error, it will be of type *PathError.
func Stat(name string) (FileInfo, error) {
	var fi FileInfo
	var err error
	for i := 0; i < 255; i++ {
		fi, err = Lstat(name)
		if err != nil {
			return nil, err
		}
		if fi.Mode()&ModeSymlink == 0 {
			return fi, nil
		}
		newname, err := Readlink(name)
		if err != nil {
			return nil, err
		}
		switch {
		case isAbs(newname):
			name = newname
		case len(newname) > 0 && IsPathSeparator(newname[0]):
			name = volumeName(name) + newname
		default:
			name = dirname(name) + `\` + newname
		}
	}
	return nil, &PathError{"Stat", name, syscall.ELOOP}
}

// Lstat returns the FileInfo structure describing the named file.
// If the file is a symbolic link, the returned FileInfo
// describes the symbolic link. Lstat makes no attempt to follow the link.
// If there is an error, it will be of type *PathError.
func Lstat(name string) (FileInfo, error) {
	if len(name) == 0 {
		return nil, &PathError{"Lstat", name, syscall.Errno(syscall.ERROR_PATH_NOT_FOUND)}
	}
	if name == DevNull {
		return &devNullStat, nil
	}
	fs := &fileStat{name: basename(name)}
	namep, e := syscall.UTF16PtrFromString(fixLongPath(name))
	if e != nil {
		return nil, &PathError{"Lstat", name, e}
	}
	e = syscall.GetFileAttributesEx(namep, syscall.GetFileExInfoStandard, (*byte)(unsafe.Pointer(&fs.sys)))
	if e != nil {
		if e != windows.ERROR_SHARING_VIOLATION {
			return nil, &PathError{"GetFileAttributesEx", name, e}
		}
		// try FindFirstFile now that GetFileAttributesEx failed
		var fd syscall.Win32finddata
		h, e2 := syscall.FindFirstFile(namep, &fd)
		if e2 != nil {
			return nil, &PathError{"FindFirstFile", name, e}
		}
		syscall.FindClose(h)

		fs.sys.FileAttributes = fd.FileAttributes
		fs.sys.CreationTime = fd.CreationTime
		fs.sys.LastAccessTime = fd.LastAccessTime
		fs.sys.LastWriteTime = fd.LastWriteTime
		fs.sys.FileSizeHigh = fd.FileSizeHigh
		fs.sys.FileSizeLow = fd.FileSizeLow
	}
	fs.path = name
	if !isAbs(fs.path) {
		fs.path, e = syscall.FullPath(fs.path)
		if e != nil {
			return nil, e
		}
	}
	return fs, nil
}
