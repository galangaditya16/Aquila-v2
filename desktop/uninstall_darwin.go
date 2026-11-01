package main

/*
#cgo CFLAGS: -x objective-c
#cgo LDFLAGS: -framework Foundation
#import <Foundation/Foundation.h>
#include <stdlib.h>
#include <string.h>

static char *aquilaTrashApp(const char *path) {
    @autoreleasepool {
        NSURL *url = [NSURL fileURLWithPath:[NSString stringWithUTF8String:path]];
        NSError *error = nil;
        if (![[NSFileManager defaultManager] trashItemAtURL:url resultingItemURL:NULL error:&error]) {
            const char *message = [[error localizedDescription] UTF8String];
            return strdup(message ? message : "Tidak dapat memindahkan aplikasi ke Trash");
        }
        return NULL;
    }
}
*/
import "C"

import (
	"fmt"
	"path/filepath"
	"strings"
	"unsafe"
)

func macAppBundle(executable string) (string, error) {
	macOS := filepath.Dir(executable)
	contents := filepath.Dir(macOS)
	bundle := filepath.Dir(contents)
	if filepath.Base(executable) != "aquila" || filepath.Base(macOS) != "MacOS" || filepath.Base(contents) != "Contents" || !strings.EqualFold(filepath.Ext(bundle), ".app") {
		return "", fmt.Errorf("jalankan Aquila dari aplikasi .app untuk memakai uninstall")
	}
	if strings.HasPrefix(bundle, "/Volumes/") {
		return "", fmt.Errorf("Aquila berjalan dari disk image. Tutup aplikasi dan eject DMG; untuk menghapus instalasi, buka Aquila dari folder Applications")
	}
	return bundle, nil
}

func prepareUninstall(executable string) (uninstallPlan, error) {
	bundle, err := macAppBundle(executable)
	if err != nil {
		return uninstallPlan{}, err
	}
	return uninstallPlan{
		description:      "Pindahkan aplikasi berikut ke Trash?\n" + bundle,
		disableAutostart: true,
		run: func() error {
			path := C.CString(bundle)
			defer C.free(unsafe.Pointer(path))
			if message := C.aquilaTrashApp(path); message != nil {
				defer C.free(unsafe.Pointer(message))
				return fmt.Errorf("gagal memindahkan Aquila ke Trash: %s", C.GoString(message))
			}
			return nil
		},
	}, nil
}
