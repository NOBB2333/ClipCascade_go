//go:build windows

package clipboard

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"image/png"
	"log/slog"
	"os/exec"
	"strings"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	kernel32                       = syscall.NewLazyDLL("kernel32.dll")
	user32                         = syscall.NewLazyDLL("user32.dll")
	shell32                        = syscall.NewLazyDLL("shell32.dll")
	procOpenClipboard              = user32.NewProc("OpenClipboard")
	procCloseClipboard             = user32.NewProc("CloseClipboard")
	procGetClipboardData           = user32.NewProc("GetClipboardData")
	procIsClipboardFormatAvailable = user32.NewProc("IsClipboardFormatAvailable")
	procGlobalLock                 = kernel32.NewProc("GlobalLock")
	procGlobalUnlock               = kernel32.NewProc("GlobalUnlock")
	procGlobalSize                 = kernel32.NewProc("GlobalSize")
	procGetClipboardSequenceNumber = user32.NewProc("GetClipboardSequenceNumber")
	procDragQueryFileW             = shell32.NewProc("DragQueryFileW")
)

const (
	CF_UNICODETEXT = 13
	CF_DIB         = 8
	CF_DIBV5       = 17
	CF_HDROP       = 15

	biRGB       = 0
	biBitfields = 3
)

type bitmapInfoHeader struct {
	Size          uint32
	Width         int32
	Height        int32
	Planes        uint16
	BitCount      uint16
	Compression   uint32
	SizeImage     uint32
	XPelsPerMeter int32
	YPelsPerMeter int32
	ClrUsed       uint32
	ClrImportant  uint32
}

// getPlatformChangeCount 返回 Windows 剪贴板的序列号。
func getPlatformChangeCount() int64 {
	r, _, _ := procGetClipboardSequenceNumber.Call()
	return int64(r)
}

// getPlatformFilePaths 查询 Windows CF_HDROP 剪贴板。
func getPlatformFilePaths() ([]string, error) {
	r, _, _ := procOpenClipboard.Call(0)
	if r == 0 {
		return nil, nil
	}
	defer procCloseClipboard.Call()

	avail, _, _ := procIsClipboardFormatAvailable.Call(CF_HDROP)
	if avail == 0 {
		return nil, nil
	}

	hDrop, _, _ := procGetClipboardData.Call(CF_HDROP)
	if hDrop == 0 {
		return nil, nil
	}

	ptr, _, _ := procGlobalLock.Call(hDrop)
	if ptr == 0 {
		return nil, nil
	}
	defer procGlobalUnlock.Call(hDrop)

	count, _, _ := procDragQueryFileW.Call(hDrop, 0xFFFFFFFF, 0, 0)
	if count == 0 || count > 1000 {
		return nil, nil
	}

	var paths []string
	for i := uintptr(0); i < count; i++ {
		size, _, _ := procDragQueryFileW.Call(hDrop, i, 0, 0)
		if size == 0 {
			continue
		}

		buf := make([]uint16, size+1)
		procDragQueryFileW.Call(hDrop, i, uintptr(unsafe.Pointer(&buf[0])), size+1)

		path := windows.UTF16ToString(buf)
		if path != "" {
			paths = append(paths, path)
		}
	}

	return paths, nil
}

func setPlatformFilePaths(paths []string) error {
	if len(paths) == 0 {
		return nil
	}
	slog.Info("剪贴板：通过 powershell 将文件路径写入 Windows 剪贴板...")

	var psPaths []string
	for _, p := range paths {
		psPaths = append(psPaths, `'`+p+`'`)
	}
	pathList := strings.Join(psPaths, ",")

	cmd := exec.Command("powershell", "-WindowStyle", "Hidden", "-NoProfile", "-Command", "Set-Clipboard", "-Path", pathList)
	cmd.SysProcAttr = &syscall.SysProcAttr{
		HideWindow:    true,
		CreationFlags: 0x08000000, // CREATE_NO_WINDOW
	}

	err := cmd.Start()
	if err == nil {
		go cmd.Wait()
	} else {
		slog.Warn("剪贴板：通过 powershell 异步写入失败", "错误", err)
	}
	return err
}

func getPlatformImage() ([]byte, error) {
	data, err := readClipboardBytes(CF_DIBV5)
	if err != nil || len(data) == 0 {
		data, err = readClipboardBytes(CF_DIB)
		if err != nil || len(data) == 0 {
			return nil, err
		}
	}

	img, err := decodeDIB(data)
	if err != nil {
		return nil, err
	}

	var out bytes.Buffer
	if err := png.Encode(&out, img); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

func getPlatformText() ([]byte, error) {
	data, err := readClipboardBytes(CF_UNICODETEXT)
	if err != nil || len(data) == 0 {
		return nil, err
	}

	if len(data)%2 != 0 {
		data = data[:len(data)-1]
	}
	u16 := unsafe.Slice((*uint16)(unsafe.Pointer(&data[0])), len(data)/2)
	text := windows.UTF16ToString(u16)
	if text == "" {
		return nil, nil
	}
	return []byte(text), nil
}

func setPlatformText(text string) error {
	return nil
}

func setPlatformImage(data []byte) error {
	return nil
}

func readClipboardBytes(format uintptr) ([]byte, error) {
	r, _, err := procOpenClipboard.Call(0)
	if r == 0 {
		return nil, err
	}
	defer procCloseClipboard.Call()

	avail, _, _ := procIsClipboardFormatAvailable.Call(format)
	if avail == 0 {
		return nil, nil
	}

	handle, _, _ := procGetClipboardData.Call(format)
	if handle == 0 {
		return nil, nil
	}

	ptr, _, _ := procGlobalLock.Call(handle)
	if ptr == 0 {
		return nil, nil
	}
	defer procGlobalUnlock.Call(handle)

	size, _, _ := procGlobalSize.Call(handle)
	if size == 0 {
		return nil, nil
	}

	buf := unsafe.Slice((*byte)(unsafe.Pointer(ptr)), int(size))
	out := make([]byte, len(buf))
	copy(out, buf)
	return out, nil
}

func decodeDIB(data []byte) (*image.NRGBA, error) {
	if len(data) < 40 {
		return nil, syscall.EINVAL
	}

	var hdr bitmapInfoHeader
	hdr.Size = binary.LittleEndian.Uint32(data[0:4])
	hdr.Width = int32(binary.LittleEndian.Uint32(data[4:8]))
	hdr.Height = int32(binary.LittleEndian.Uint32(data[8:12]))
	hdr.Planes = binary.LittleEndian.Uint16(data[12:14])
	hdr.BitCount = binary.LittleEndian.Uint16(data[14:16])
	hdr.Compression = binary.LittleEndian.Uint32(data[16:20])
	hdr.SizeImage = binary.LittleEndian.Uint32(data[20:24])
	hdr.ClrUsed = binary.LittleEndian.Uint32(data[32:36])

	if hdr.Planes != 1 || hdr.Width == 0 || hdr.Height == 0 {
		return nil, syscall.EINVAL
	}

	width := int(hdr.Width)
	height := int(hdr.Height)
	topDown := false
	if height < 0 {
		topDown = true
		height = -height
	}

	offset := int(hdr.Size)
	switch hdr.BitCount {
	case 32:
		if hdr.Compression == biBitfields {
			offset += 12
		}
	case 24:
		// no-op
	default:
		return nil, syscall.EINVAL
	}

	if offset >= len(data) {
		return nil, syscall.EINVAL
	}

	rowStride := ((width*int(hdr.BitCount) + 31) / 32) * 4
	required := offset + rowStride*height
	if required > len(data) {
		return nil, syscall.EINVAL
	}

	img := image.NewNRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		srcY := y
		if !topDown {
			srcY = height - 1 - y
		}
		row := data[offset+srcY*rowStride : offset+(srcY+1)*rowStride]
		for x := 0; x < width; x++ {
			switch hdr.BitCount {
			case 32:
				i := x * 4
				b, g, r, a := row[i], row[i+1], row[i+2], row[i+3]
				if a == 0 {
					a = 255
				}
				img.SetNRGBA(x, y, color.NRGBA{R: r, G: g, B: b, A: a})
			case 24:
				i := x * 3
				b, g, r := row[i], row[i+1], row[i+2]
				img.SetNRGBA(x, y, color.NRGBA{R: r, G: g, B: b, A: 255})
			}
		}
	}

	return img, nil
}
