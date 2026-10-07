//go:build windows

package convert

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

// The Windows Imaging Component is the part of Windows that reads and writes pictures.
// It is used here through COM, without any library in between. The numbers passed to
// method are positions in the method tables of wincodec.h in the Windows SDK.
//
// Every address that goes to Windows is converted to uintptr inside the call to
// syscall.SyscallN or Proc.Call itself. That is the form in which Go keeps the value it
// points at alive, and in place, for the length of the call.

var (
	ole32                 = windows.NewLazySystemDLL("ole32.dll")
	shlwapi               = windows.NewLazySystemDLL("shlwapi.dll")
	procCoInitializeEx    = ole32.NewProc("CoInitializeEx")
	procCoUninitialize    = ole32.NewProc("CoUninitialize")
	procCoCreateInstance  = ole32.NewProc("CoCreateInstance")
	procSHCreateMemStream = shlwapi.NewProc("SHCreateMemStream")

	clsidImagingFactory = windows.GUID{Data1: 0xcacaf262, Data2: 0x9370, Data3: 0x4615, Data4: [8]byte{0xa1, 0x3b, 0x9f, 0x55, 0x39, 0xda, 0x4c, 0x0a}}
	iidImagingFactory   = windows.GUID{Data1: 0xec5ec8a9, Data2: 0xc395, Data3: 0x4314, Data4: [8]byte{0x9c, 0x77, 0x54, 0xd7, 0xa9, 0x35, 0xff, 0x70}}
	containerPNG        = windows.GUID{Data1: 0x1b7cfaf4, Data2: 0x713f, Data3: 0x473c, Data4: [8]byte{0xbb, 0xcd, 0x61, 0x37, 0x42, 0x5f, 0xae, 0xaf}}
	pixels24bppBGR      = windows.GUID{Data1: 0x6fddc324, Data2: 0x4e03, Data3: 0x4bfe, Data4: [8]byte{0xb1, 0x85, 0x3d, 0x77, 0x76, 0x8d, 0xc9, 0x0c}}
	pixels32bppBGRA     = windows.GUID{Data1: 0x6fddc324, Data2: 0x4e03, Data3: 0x4bfe, Data4: [8]byte{0xb1, 0x85, 0x3d, 0x77, 0x76, 0x8d, 0xc9, 0x0f}}

	// transparentPixels are the pixel layouts with transparency that a decoder may hand out.
	// Anything else is written without it.
	transparentPixels = map[windows.GUID]bool{
		pixels32bppBGRA: true,
		{Data1: 0x6fddc324, Data2: 0x4e03, Data3: 0x4bfe, Data4: [8]byte{0xb1, 0x85, 0x3d, 0x77, 0x76, 0x8d, 0xc9, 0x10}}: true, // 32bppPBGRA
		{Data1: 0xf5c7ad2d, Data2: 0x6a8d, Data3: 0x43dd, Data4: [8]byte{0xa7, 0xa8, 0xa2, 0x99, 0x35, 0x26, 0x1a, 0xe9}}: true, // 32bppRGBA
		{Data1: 0x3cc4a650, Data2: 0xa527, Data3: 0x4d37, Data4: [8]byte{0xa9, 0x16, 0x31, 0x42, 0xc7, 0xeb, 0xed, 0xba}}: true, // 32bppPRGBA
		{Data1: 0x6fddc324, Data2: 0x4e03, Data3: 0x4bfe, Data4: [8]byte{0xb1, 0x85, 0x3d, 0x77, 0x76, 0x8d, 0xc9, 0x16}}: true, // 64bppRGBA
		{Data1: 0x1562ff7c, Data2: 0xd352, Data3: 0x46f9, Data4: [8]byte{0x97, 0x9e, 0x42, 0x97, 0x6b, 0x79, 0x22, 0x46}}: true, // 64bppBGRA
		{Data1: 0x6fddc324, Data2: 0x4e03, Data3: 0x4bfe, Data4: [8]byte{0xb1, 0x85, 0x3d, 0x77, 0x76, 0x8d, 0xc9, 0x17}}: true, // 64bppPRGBA
	}
)

const (
	comMultithreaded   = 0x0
	comChangedMode     = 0x80010106 // this thread already uses COM in another mode, which is fine
	comInProcessServer = 0x1
	genericWrite       = 0x40000000
	encoderNoCache     = 0x2
	scaleFant          = 0x3
	colorsSRGB         = 0x1        // the EXIF number for the sRGB colour space
	colorsAsProfile    = 0x1        // WICColorContextProfile: the colours are described by an ICC profile
	noColorsStored     = 0x88982F80 // WINCODEC_ERR_UNSUPPORTEDOPERATION: this kind of picture never has a profile
)

// hresult is a failure code from Windows.
type hresult uint32

func (h hresult) Error() string { return fmt.Sprintf("Windows imaging error 0x%08X", uint32(h)) }

// check turns the result of a COM call into an error.
func check(result, _ uintptr, _ error) error {
	if int32(result) < 0 {
		return hresult(result)
	}
	return nil
}

// comObject is a COM interface that this code has to release.
type comObject struct{ ptr unsafe.Pointer }

// method returns the address of the method in the given position of the interface.
func (o *comObject) method(position int) uintptr {
	table := *(*unsafe.Pointer)(o.ptr)
	return *(*uintptr)(unsafe.Add(table, uintptr(position)*unsafe.Sizeof(uintptr(0))))
}

func (o *comObject) release() {
	if o.ptr != nil {
		_, _, _ = syscall.SyscallN(o.method(2), uintptr(o.ptr))
		o.ptr = nil
	}
}

// withImaging runs work with the imaging factory of Windows. COM belongs to a thread, so
// the goroutine stays on one thread until the work is done.
func withImaging(work func(factory *comObject) error) error {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	started, _, _ := procCoInitializeEx.Call(0, comMultithreaded)
	switch {
	case int32(started) >= 0:
		defer func() { _, _, _ = procCoUninitialize.Call() }()
	case uint32(started) != comChangedMode:
		return hresult(started)
	}
	var factory comObject
	if err := check(procCoCreateInstance.Call(uintptr(unsafe.Pointer(&clsidImagingFactory)), 0, comInProcessServer,
		uintptr(unsafe.Pointer(&iidImagingFactory)), uintptr(unsafe.Pointer(&factory.ptr)))); err != nil {
		return err
	}
	defer factory.release()
	return work(&factory)
}

type windowsImages struct{}

func newSystemImages() SystemImages { return windowsImages{} }

// readable makes an error from this file fit for the row of a file.
func readable(err error) error {
	var code hresult
	if errors.As(err, &code) {
		return &SystemImageError{Message: heicUnreadable, Detail: code.Error()}
	}
	return &SystemImageError{Message: sentence(err.Error())}
}

func (windowsImages) Export(input, output string, maxSide int) (width, height int, err error) {
	if !filepath.IsAbs(input) || !filepath.IsAbs(output) {
		return 0, 0, readable(errors.New("the file path must be absolute"))
	}
	target, err := windows.UTF16PtrFromString(output)
	if err != nil {
		return 0, 0, readable(errors.New("the work file has a name that cannot be used"))
	}
	// Go opens the file, so a long path or an odd name is no different from any other. The
	// handle has to stay open for as long as the decoder exists.
	file, err := os.Open(input)
	if err != nil {
		return 0, 0, readable(errors.New("the file could not be opened"))
	}
	defer file.Close()
	err = withImaging(func(factory *comObject) error {
		var decoder comObject
		// CreateDecoderFromFileHandle(file, any vendor, read metadata when asked, decoder)
		if err := check(syscall.SyscallN(factory.method(5), uintptr(factory.ptr), file.Fd(), 0, 0,
			uintptr(unsafe.Pointer(&decoder.ptr)))); err != nil {
			return err
		}
		defer decoder.release()
		width, height, err = writePNG(factory, &decoder, target, maxSide)
		return err
	})
	if err != nil {
		_ = os.Remove(output)
		return 0, 0, readable(err)
	}
	return width, height, nil
}

// writePNG writes the first picture of a decoder to a PNG file. Windows has already
// turned and cropped the picture the way the file asks for.
func writePNG(factory, decoder *comObject, target *uint16, maxSide int) (int, int, error) {
	var frame comObject
	// IWICBitmapDecoder::GetFrame(0, frame)
	if err := check(syscall.SyscallN(decoder.method(13), uintptr(decoder.ptr), 0, uintptr(unsafe.Pointer(&frame.ptr)))); err != nil {
		return 0, 0, err
	}
	defer frame.release()
	var fullWidth, fullHeight uint32
	// IWICBitmapSource::GetSize(width, height)
	if err := check(syscall.SyscallN(frame.method(3), uintptr(frame.ptr),
		uintptr(unsafe.Pointer(&fullWidth)), uintptr(unsafe.Pointer(&fullHeight)))); err != nil {
		return 0, 0, err
	}
	width, height := int(fullWidth), int(fullHeight)
	if err := checkImageSize(width, height); err != nil {
		return 0, 0, err
	}
	var layout windows.GUID
	// IWICBitmapSource::GetPixelFormat(format)
	if err := check(syscall.SyscallN(frame.method(4), uintptr(frame.ptr), uintptr(unsafe.Pointer(&layout)))); err != nil {
		return 0, 0, err
	}
	wanted := pixels24bppBGR
	if transparentPixels[layout] {
		wanted = pixels32bppBGRA
	}

	source := &frame
	outWidth, outHeight := scaledSize(width, height, maxSide)
	if outWidth != width || outHeight != height {
		var scaler comObject
		// IWICImagingFactory::CreateBitmapScaler(scaler)
		if err := check(syscall.SyscallN(factory.method(11), uintptr(factory.ptr), uintptr(unsafe.Pointer(&scaler.ptr)))); err != nil {
			return 0, 0, err
		}
		defer scaler.release()
		// IWICBitmapScaler::Initialize(source, width, height, mode)
		if err := check(syscall.SyscallN(scaler.method(8), uintptr(scaler.ptr), uintptr(source.ptr),
			uintptr(outWidth), uintptr(outHeight), scaleFant)); err != nil {
			return 0, 0, err
		}
		source = &scaler
	}

	var converter comObject
	// IWICImagingFactory::CreateFormatConverter(converter)
	if err := check(syscall.SyscallN(factory.method(10), uintptr(factory.ptr), uintptr(unsafe.Pointer(&converter.ptr)))); err != nil {
		return 0, 0, err
	}
	defer converter.release()
	// IWICFormatConverter::Initialize(source, format, no dithering, no palette, 0.0, custom palette)
	if err := check(syscall.SyscallN(converter.method(8), uintptr(converter.ptr), uintptr(source.ptr),
		uintptr(unsafe.Pointer(&wanted)), 0, 0, 0, 0)); err != nil {
		return 0, 0, err
	}
	source = &converter

	// Phone photos are stored in a wider colour space than the one that picture formats
	// without a colour profile are understood to be in. Windows hands the colours over as
	// they are stored, so they are brought to sRGB here. Without this step every result
	// would look a little paler than the photo.
	colors, err := frameColors(factory, &frame)
	if err != nil {
		return 0, 0, err
	}
	if colors != nil {
		defer colors.release()
		var standard, transform comObject
		// IWICImagingFactory::CreateColorContext(context)
		if err := check(syscall.SyscallN(factory.method(15), uintptr(factory.ptr), uintptr(unsafe.Pointer(&standard.ptr)))); err != nil {
			return 0, 0, err
		}
		defer standard.release()
		// IWICColorContext::InitializeFromExifColorSpace(sRGB)
		if err := check(syscall.SyscallN(standard.method(5), uintptr(standard.ptr), colorsSRGB)); err != nil {
			return 0, 0, err
		}
		// IWICImagingFactory::CreateColorTransformer(transform)
		if err := check(syscall.SyscallN(factory.method(16), uintptr(factory.ptr), uintptr(unsafe.Pointer(&transform.ptr)))); err != nil {
			return 0, 0, err
		}
		defer transform.release()
		// IWICColorTransform::Initialize(source, colours of the source, colours wanted, format)
		if err := check(syscall.SyscallN(transform.method(8), uintptr(transform.ptr), uintptr(source.ptr),
			uintptr(colors.ptr), uintptr(standard.ptr), uintptr(unsafe.Pointer(&wanted)))); err != nil {
			return 0, 0, err
		}
		source = &transform
	}

	var stream comObject
	// IWICImagingFactory::CreateStream(stream)
	if err := check(syscall.SyscallN(factory.method(14), uintptr(factory.ptr), uintptr(unsafe.Pointer(&stream.ptr)))); err != nil {
		return 0, 0, err
	}
	defer stream.release()
	// IWICStream::InitializeFromFilename(name, access)
	if err := check(syscall.SyscallN(stream.method(15), uintptr(stream.ptr), uintptr(unsafe.Pointer(target)), genericWrite)); err != nil {
		return 0, 0, err
	}
	var encoder comObject
	// IWICImagingFactory::CreateEncoder(container, any vendor, encoder)
	if err := check(syscall.SyscallN(factory.method(8), uintptr(factory.ptr), uintptr(unsafe.Pointer(&containerPNG)), 0,
		uintptr(unsafe.Pointer(&encoder.ptr)))); err != nil {
		return 0, 0, err
	}
	defer encoder.release()
	// IWICBitmapEncoder::Initialize(stream, cache option)
	if err := check(syscall.SyscallN(encoder.method(3), uintptr(encoder.ptr), uintptr(stream.ptr), encoderNoCache)); err != nil {
		return 0, 0, err
	}
	var page comObject
	// IWICBitmapEncoder::CreateNewFrame(frame, no options)
	if err := check(syscall.SyscallN(encoder.method(10), uintptr(encoder.ptr), uintptr(unsafe.Pointer(&page.ptr)), 0)); err != nil {
		return 0, 0, err
	}
	defer page.release()
	// IWICBitmapFrameEncode::Initialize(no options)
	if err := check(syscall.SyscallN(page.method(3), uintptr(page.ptr), 0)); err != nil {
		return 0, 0, err
	}
	// IWICBitmapFrameEncode::SetSize(width, height)
	if err := check(syscall.SyscallN(page.method(4), uintptr(page.ptr), uintptr(outWidth), uintptr(outHeight))); err != nil {
		return 0, 0, err
	}
	written := wanted
	// IWICBitmapFrameEncode::SetPixelFormat(format). The encoder may answer with another
	// layout, and WriteSource then converts to it.
	if err := check(syscall.SyscallN(page.method(6), uintptr(page.ptr), uintptr(unsafe.Pointer(&written)))); err != nil {
		return 0, 0, err
	}
	// IWICBitmapFrameEncode::WriteSource(source, whole picture)
	if err := check(syscall.SyscallN(page.method(11), uintptr(page.ptr), uintptr(source.ptr), 0)); err != nil {
		return 0, 0, err
	}
	// IWICBitmapFrameEncode::Commit()
	if err := check(syscall.SyscallN(page.method(12), uintptr(page.ptr))); err != nil {
		return 0, 0, err
	}
	// IWICBitmapEncoder::Commit()
	if err := check(syscall.SyscallN(encoder.method(11), uintptr(encoder.ptr))); err != nil {
		return 0, 0, err
	}
	return width, height, nil
}

// frameColors returns the ICC colour profile that a picture carries, or nil when it has
// none. The caller releases it.
func frameColors(factory, frame *comObject) (*comObject, error) {
	var count uint32
	// IWICBitmapFrameDecode::GetColorContexts(0, nothing, count) asks how many there are.
	if err := check(syscall.SyscallN(frame.method(9), uintptr(frame.ptr), 0, 0, uintptr(unsafe.Pointer(&count)))); err != nil {
		var code hresult
		if errors.As(err, &code) && uint32(code) == noColorsStored {
			return nil, nil
		}
		return nil, err
	}
	if count == 0 {
		return nil, nil
	}
	contexts := make([]comObject, count)
	defer func() {
		for i := range contexts {
			contexts[i].release()
		}
	}()
	addresses := make([]unsafe.Pointer, count)
	for i := range contexts {
		// IWICImagingFactory::CreateColorContext(context)
		if err := check(syscall.SyscallN(factory.method(15), uintptr(factory.ptr), uintptr(unsafe.Pointer(&contexts[i].ptr)))); err != nil {
			return nil, err
		}
		addresses[i] = contexts[i].ptr
	}
	// IWICBitmapFrameDecode::GetColorContexts(count, contexts, count) fills them in.
	if err := check(syscall.SyscallN(frame.method(9), uintptr(frame.ptr), uintptr(count),
		uintptr(unsafe.Pointer(&addresses[0])), uintptr(unsafe.Pointer(&count)))); err != nil {
		return nil, err
	}
	for i := 0; i < int(count) && i < len(contexts); i++ {
		var kind uint32
		// IWICColorContext::GetType(kind)
		if err := check(syscall.SyscallN(contexts[i].method(6), uintptr(contexts[i].ptr), uintptr(unsafe.Pointer(&kind)))); err != nil {
			return nil, err
		}
		if kind == colorsAsProfile {
			found := contexts[i]
			contexts[i].ptr = nil
			return &found, nil
		}
	}
	return nil, nil
}

// Check decodes the HEIC picture that is part of the app, from memory. Nothing is written.
func (windowsImages) Check() error {
	return withImaging(func(factory *comObject) error {
		var stream comObject
		// SHCreateMemStream copies the bytes and returns a stream, or nothing. The stream is
		// memory of Windows, so its address is taken over as it is.
		created, _, _ := procSHCreateMemStream.Call(uintptr(unsafe.Pointer(&heicCheck[0])), uintptr(len(heicCheck)))
		if created == 0 {
			return errors.New("no memory for the HEIC check")
		}
		stream.ptr = *(*unsafe.Pointer)(unsafe.Pointer(&created))
		defer stream.release()
		var decoder comObject
		// IWICImagingFactory::CreateDecoderFromStream(stream, any vendor, read metadata when asked, decoder)
		if err := check(syscall.SyscallN(factory.method(4), uintptr(factory.ptr), uintptr(stream.ptr), 0, 0,
			uintptr(unsafe.Pointer(&decoder.ptr)))); err != nil {
			return err
		}
		defer decoder.release()
		var frame comObject
		if err := check(syscall.SyscallN(decoder.method(13), uintptr(decoder.ptr), 0, uintptr(unsafe.Pointer(&frame.ptr)))); err != nil {
			return err
		}
		defer frame.release()
		var width, height uint32
		if err := check(syscall.SyscallN(frame.method(3), uintptr(frame.ptr),
			uintptr(unsafe.Pointer(&width)), uintptr(unsafe.Pointer(&height)))); err != nil {
			return err
		}
		if width == 0 || height == 0 || width > 4096 || height > 4096 {
			return errors.New("the HEIC check picture has an unexpected size")
		}
		var converter comObject
		if err := check(syscall.SyscallN(factory.method(10), uintptr(factory.ptr), uintptr(unsafe.Pointer(&converter.ptr)))); err != nil {
			return err
		}
		defer converter.release()
		if err := check(syscall.SyscallN(converter.method(8), uintptr(converter.ptr), uintptr(frame.ptr),
			uintptr(unsafe.Pointer(&pixels32bppBGRA)), 0, 0, 0, 0)); err != nil {
			return err
		}
		// Reading the pixels is the step that needs the HEVC decoder of Windows.
		stride := width * 4
		pixels := make([]byte, stride*height)
		// IWICBitmapSource::CopyPixels(whole picture, stride, buffer size, buffer)
		return check(syscall.SyscallN(converter.method(7), uintptr(converter.ptr), 0, uintptr(stride), uintptr(len(pixels)),
			uintptr(unsafe.Pointer(&pixels[0]))))
	})
}
