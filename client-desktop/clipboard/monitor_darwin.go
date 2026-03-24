//go:build darwin

package clipboard

/*
#cgo CFLAGS: -x objective-c
#cgo LDFLAGS: -framework Foundation -framework AppKit

#import <Foundation/Foundation.h>
#import <AppKit/AppKit.h>
#import <dispatch/dispatch.h>

static void* copyNSDataBytes(NSData *data) {
    if (data == nil || data.length == 0) {
        return NULL;
    }
    void *buffer = malloc((size_t)data.length);
    if (buffer == NULL) {
        return NULL;
    }
    memcpy(buffer, data.bytes, (size_t)data.length);
    return buffer;
}

static const char* copyFilePathsFromPasteboard(void) {
    @autoreleasepool {
        NSPasteboard *pb = [NSPasteboard generalPasteboard];

        // 读取 public.file-url（现代标准格式，支持 Finder 拖拽、沙盒 app 等所有场景）
        NSArray *classes = @[[NSURL class]];
        NSDictionary *opts = @{NSPasteboardURLReadingFileURLsOnlyKey: @YES};
        NSArray *urls = [pb readObjectsForClasses:classes options:opts];
        if ([urls isKindOfClass:[NSArray class]] && urls.count > 0) {
            NSMutableArray *paths = [NSMutableArray arrayWithCapacity:urls.count];
            for (NSURL *url in urls) {
                if (url.path) {
                    [paths addObject:url.path];
                }
            }
            if (paths.count > 0) {
                NSString *joined = [paths componentsJoinedByString:@"\n"];
                return strdup([joined UTF8String]);
            }
        }

        return strdup("");
    }
}

typedef struct {
    const char *result;
} FilePathsContext;

static void copyFilePathsOnMainThread(void *ctx) {
    FilePathsContext *context = (FilePathsContext *)ctx;
    context->result = copyFilePathsFromPasteboard();
}

typedef struct {
    const char *path;
} SetFilePathContext;

typedef struct {
    long long count;
} ChangeCountContext;

typedef struct {
    void *data;
    unsigned long long length;
} ClipboardBufferContext;

static void copyChangeCountOnMainThread(void *ctx) {
    ChangeCountContext *context = (ChangeCountContext *)ctx;
    @autoreleasepool {
        context->count = (long long)[[NSPasteboard generalPasteboard] changeCount];
    }
}

static void readTextOnMainThread(void *ctx) {
    ClipboardBufferContext *context = (ClipboardBufferContext *)ctx;
    @autoreleasepool {
        NSPasteboard *pb = [NSPasteboard generalPasteboard];
        NSString *text = [pb stringForType:NSPasteboardTypeString];
        if (text == nil || text.length == 0) {
            context->data = NULL;
            context->length = 0;
            return;
        }

        NSData *utf8 = [text dataUsingEncoding:NSUTF8StringEncoding];
        context->data = copyNSDataBytes(utf8);
        context->length = (unsigned long long)utf8.length;
    }
}

static void readImageOnMainThread(void *ctx) {
    ClipboardBufferContext *context = (ClipboardBufferContext *)ctx;
    @autoreleasepool {
        NSPasteboard *pb = [NSPasteboard generalPasteboard];
        NSArray *classes = @[[NSImage class]];
        NSArray *objects = [pb readObjectsForClasses:classes options:nil];
        if (![objects isKindOfClass:[NSArray class]] || objects.count == 0) {
            context->data = NULL;
            context->length = 0;
            return;
        }

        NSImage *image = objects.firstObject;
        if (image == nil) {
            context->data = NULL;
            context->length = 0;
            return;
        }

        CGImageRef cgImage = [image CGImageForProposedRect:NULL context:nil hints:nil];
        if (cgImage == NULL) {
            context->data = NULL;
            context->length = 0;
            return;
        }

        NSBitmapImageRep *rep = [[NSBitmapImageRep alloc] initWithCGImage:cgImage];
        NSData *png = [rep representationUsingType:NSBitmapImageFileTypePNG properties:@{}];
        context->data = copyNSDataBytes(png);
        context->length = (unsigned long long)png.length;
    }
}

static void setFilePathOnMainThread(void *ctx) {
    SetFilePathContext *context = (SetFilePathContext *)ctx;
    @autoreleasepool {
        NSString *nsPath = [NSString stringWithUTF8String:context->path];
        NSURL *url = [NSURL fileURLWithPath:nsPath];
        if (!url) return;
        NSPasteboard *pb = [NSPasteboard generalPasteboard];
        [pb clearContents];
        [pb writeObjects:@[url]];
    }
}

static void writeTextOnMainThread(void *ctx) {
    ClipboardBufferContext *context = (ClipboardBufferContext *)ctx;
    @autoreleasepool {
        NSPasteboard *pb = [NSPasteboard generalPasteboard];
        [pb clearContents];
        if (context->data == NULL || context->length == 0) {
            return;
        }
        NSString *text = [[NSString alloc] initWithBytes:context->data
                                                  length:(NSUInteger)context->length
                                                encoding:NSUTF8StringEncoding];
        if (text != nil) {
            [pb setString:text forType:NSPasteboardTypeString];
        }
    }
}

static void writeImageOnMainThread(void *ctx) {
    ClipboardBufferContext *context = (ClipboardBufferContext *)ctx;
    @autoreleasepool {
        NSPasteboard *pb = [NSPasteboard generalPasteboard];
        [pb clearContents];
        if (context->data == NULL || context->length == 0) {
            return;
        }
        NSData *data = [NSData dataWithBytes:context->data length:(NSUInteger)context->length];
        NSImage *image = [[NSImage alloc] initWithData:data];
        if (image != nil) {
            [pb writeObjects:@[image]];
        }
    }
}

void readClipboardText(void *ctx) {
    if ([NSThread isMainThread]) {
        readTextOnMainThread(ctx);
        return;
    }
    dispatch_sync_f(dispatch_get_main_queue(), ctx, readTextOnMainThread);
}

void readClipboardImage(void *ctx) {
    if ([NSThread isMainThread]) {
        readImageOnMainThread(ctx);
        return;
    }
    dispatch_sync_f(dispatch_get_main_queue(), ctx, readImageOnMainThread);
}

void writeClipboardText(void *ctx) {
    if ([NSThread isMainThread]) {
        writeTextOnMainThread(ctx);
        return;
    }
    dispatch_sync_f(dispatch_get_main_queue(), ctx, writeTextOnMainThread);
}

void writeClipboardImage(void *ctx) {
    if ([NSThread isMainThread]) {
        writeImageOnMainThread(ctx);
        return;
    }
    dispatch_sync_f(dispatch_get_main_queue(), ctx, writeImageOnMainThread);
}

// getChangeCount 返回 NSPasteboard 的 changeCount，用于检测剪贴板是否发生变化。
// 每次剪贴板内容更改时，changeCount 会递增。调用无副作用，耗时 <1µs。
long long getChangeCount() {
    if ([NSThread isMainThread]) {
        @autoreleasepool {
            return (long long)[[NSPasteboard generalPasteboard] changeCount];
        }
    }

    ChangeCountContext ctx = {0};
    dispatch_sync_f(dispatch_get_main_queue(), &ctx, copyChangeCountOnMainThread);
    return ctx.count;
}

// getFilePaths 返回剪贴板中的文件路径列表（换行符分隔）。
// 使用现代 NSPasteboardTypeFileURL API（macOS 10.14+），不使用已废弃的 NSFilenamesPboardType。
// 如果剪贴板中没有文件，返回空字符串。
const char* getFilePaths() {
    if ([NSThread isMainThread]) {
        return copyFilePathsFromPasteboard();
    }

    FilePathsContext ctx = {0};
    dispatch_sync_f(dispatch_get_main_queue(), &ctx, copyFilePathsOnMainThread);
    return ctx.result;
}


// setFilePath 将单个文件路径写入 macOS 剪贴板（file URL 格式）。
void setFilePath(const char* path) {
    if ([NSThread isMainThread]) {
        SetFilePathContext ctx = {path};
        setFilePathOnMainThread(&ctx);
        return;
    }

    SetFilePathContext ctx = {path};
    dispatch_sync_f(dispatch_get_main_queue(), &ctx, setFilePathOnMainThread);
}
*/
import "C"
import (
	"log/slog"
	"os"
	"strings"
	"unsafe"
)

// getPlatformChangeCount 通过 NSPasteboard.changeCount 检测剪贴板变化。
// 直接调用原生 API，无进程开销，耗时 <1µs。
func getPlatformChangeCount() int64 {
	return int64(C.getChangeCount())
}

func getPlatformText() ([]byte, error) {
	ctx := C.ClipboardBufferContext{}
	C.readClipboardText(unsafe.Pointer(&ctx))
	if ctx.data == nil || ctx.length == 0 {
		return nil, nil
	}
	defer C.free(ctx.data)
	return C.GoBytes(ctx.data, C.int(ctx.length)), nil
}

func getPlatformImage() ([]byte, error) {
	ctx := C.ClipboardBufferContext{}
	C.readClipboardImage(unsafe.Pointer(&ctx))
	if ctx.data == nil || ctx.length == 0 {
		return nil, nil
	}
	defer C.free(ctx.data)
	return C.GoBytes(ctx.data, C.int(ctx.length)), nil
}

// getPlatformFilePaths 通过 NSPasteboard 原生 API 获取剪贴板中的文件路径列表。
// 完全替代原有的 osascript 子进程方案，消除每次调用产生新进程的 CPU 开销。
func getPlatformFilePaths() ([]string, error) {
	cStr := C.getFilePaths()
	defer C.free(unsafe.Pointer(cStr))

	raw := strings.TrimSpace(C.GoString(cStr))
	if raw == "" {
		return nil, nil
	}

	parts := strings.Split(raw, "\n")
	var validPaths []string
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		// 严格磁盘校验，确保路径真实存在
		if _, err := os.Stat(p); err == nil {
			validPaths = append(validPaths, p)
		}
	}
	return validPaths, nil
}

// setPlatformFilePaths 通过 NSPasteboard 原生 API 将文件路径写入 macOS 剪贴板。
// 替代原有的 osascript 子进程方案。目前仅支持写入第一个路径（与原逻辑一致）。
func setPlatformFilePaths(paths []string) error {
	if len(paths) == 0 {
		return nil
	}
	slog.Info("剪贴板：通过 NSPasteboard 将文件路径写入 macOS 剪贴板...", "路径", paths[0])
	cPath := C.CString(paths[0])
	defer C.free(unsafe.Pointer(cPath))
	C.setFilePath(cPath)
	return nil
}

func setPlatformText(text string) error {
	var ptr unsafe.Pointer
	if text != "" {
		cText := C.CBytes([]byte(text))
		defer C.free(cText)
		ptr = cText
	}
	ctx := C.ClipboardBufferContext{
		data:   ptr,
		length: C.ulonglong(len(text)),
	}
	C.writeClipboardText(unsafe.Pointer(&ctx))
	return nil
}

func setPlatformImage(data []byte) error {
	var ptr unsafe.Pointer
	if len(data) > 0 {
		cData := C.CBytes(data)
		defer C.free(cData)
		ptr = cData
	}
	ctx := C.ClipboardBufferContext{
		data:   ptr,
		length: C.ulonglong(len(data)),
	}
	C.writeClipboardImage(unsafe.Pointer(&ctx))
	return nil
}
