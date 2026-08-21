//go:build windows

package main

/*
#cgo windows CFLAGS: -DCOBJMACROS
#cgo windows LDFLAGS: -lole32 -lwindowscodecs -loleaut32

#include <windows.h>
#include <objbase.h>
#include <wincodec.h>
#include <propidl.h>
#include <stdlib.h>

static void convertme_release_unknown(IUnknown *unknown) {
	if (unknown != NULL) {
		unknown->lpVtbl->Release(unknown);
	}
}

static HRESULT convertme_create_factory(IWICImagingFactory **factory) {
	return CoCreateInstance(
		&CLSID_WICImagingFactory,
		NULL,
		CLSCTX_INPROC_SERVER,
		&IID_IWICImagingFactory,
		(void **)factory
	);
}

static int convertme_heic_codec_available(void) {
	HRESULT initResult = CoInitializeEx(NULL, COINIT_MULTITHREADED);
	int shouldUninitialize = SUCCEEDED(initResult);
	IWICImagingFactory *factory = NULL;
	IWICBitmapEncoder *encoder = NULL;
	HRESULT result = convertme_create_factory(&factory);
	if (SUCCEEDED(result)) {
		result = factory->lpVtbl->CreateEncoder(factory, &GUID_ContainerFormatHeif, NULL, &encoder);
	}
	if (encoder != NULL) {
		convertme_release_unknown((IUnknown *)encoder);
	}
	if (factory != NULL) {
		convertme_release_unknown((IUnknown *)factory);
	}
	if (shouldUninitialize) {
		CoUninitialize();
	}
	return SUCCEEDED(result) ? 1 : 0;
}

static HRESULT convertme_encode_heic(
	const BYTE *bgra,
	UINT width,
	UINT height,
	const char *utf8Path,
	UINT quality
) {
	HRESULT initResult = CoInitializeEx(NULL, COINIT_MULTITHREADED);
	int shouldUninitialize = SUCCEEDED(initResult);
	HRESULT result = S_OK;
	WCHAR path[MAX_PATH * 4];
	int pathLength = MultiByteToWideChar(CP_UTF8, 0, utf8Path, -1, path, sizeof(path) / sizeof(path[0]));
	if (pathLength == 0) {
		result = HRESULT_FROM_WIN32(GetLastError());
		goto cleanup;
	}

	IWICImagingFactory *factory = NULL;
	IWICStream *stream = NULL;
	IWICBitmapEncoder *encoder = NULL;
	IWICBitmapFrameEncode *frame = NULL;
	IWICBitmap *bitmap = NULL;
	IWICFormatConverter *converter = NULL;
	IPropertyBag2 *properties = NULL;

	result = convertme_create_factory(&factory);
	if (FAILED(result)) {
		goto release_objects;
	}
	result = factory->lpVtbl->CreateStream(factory, &stream);
	if (FAILED(result)) {
		goto release_objects;
	}
	result = stream->lpVtbl->InitializeFromFilename(stream, path, GENERIC_WRITE);
	if (FAILED(result)) {
		goto release_objects;
	}
	result = factory->lpVtbl->CreateEncoder(factory, &GUID_ContainerFormatHeif, NULL, &encoder);
	if (FAILED(result)) {
		goto release_objects;
	}
	result = encoder->lpVtbl->Initialize(encoder, (IStream *)stream, WICBitmapEncoderNoCache);
	if (FAILED(result)) {
		goto release_objects;
	}
	result = encoder->lpVtbl->CreateNewFrame(encoder, &frame, &properties);
	if (FAILED(result)) {
		goto release_objects;
	}

	if (properties != NULL) {
		PROPBAG2 property = {0};
		property.pstrName = L"ImageQuality";
		VARIANT value;
		VariantInit(&value);
		value.vt = VT_R4;
		value.fltVal = (FLOAT)quality / 100.0f;
		properties->lpVtbl->Write(properties, 1, &property, &value);
		VariantClear(&value);
	}

	result = frame->lpVtbl->Initialize(frame, properties);
	if (FAILED(result)) {
		goto release_objects;
	}
	result = frame->lpVtbl->SetSize(frame, width, height);
	if (FAILED(result)) {
		goto release_objects;
	}

	WICPixelFormatGUID pixelFormat = GUID_WICPixelFormat32bppBGRA;
	result = frame->lpVtbl->SetPixelFormat(frame, &pixelFormat);
	if (FAILED(result)) {
		goto release_objects;
	}
	result = factory->lpVtbl->CreateBitmapFromMemory(
		factory,
		width,
		height,
		&GUID_WICPixelFormat32bppBGRA,
		width * 4,
		width * height * 4,
		(BYTE *)bgra,
		&bitmap
	);
	if (FAILED(result)) {
		goto release_objects;
	}
	if (IsEqualGUID(&pixelFormat, &GUID_WICPixelFormat32bppBGRA)) {
		result = frame->lpVtbl->WriteSource(frame, (IWICBitmapSource *)bitmap, NULL);
	} else {
		BOOL canConvert = FALSE;
		result = factory->lpVtbl->CreateFormatConverter(factory, &converter);
		if (FAILED(result)) {
			goto release_objects;
		}
		result = converter->lpVtbl->CanConvert(
			converter,
			&GUID_WICPixelFormat32bppBGRA,
			&pixelFormat,
			&canConvert
		);
		if (FAILED(result) || !canConvert) {
			if (SUCCEEDED(result)) {
				result = WINCODEC_ERR_UNSUPPORTEDPIXELFORMAT;
			}
			goto release_objects;
		}
		result = converter->lpVtbl->Initialize(
			converter,
			(IWICBitmapSource *)bitmap,
			&pixelFormat,
			WICBitmapDitherTypeNone,
			NULL,
			0.0,
			WICBitmapPaletteTypeCustom
		);
		if (SUCCEEDED(result)) {
			result = frame->lpVtbl->WriteSource(frame, (IWICBitmapSource *)converter, NULL);
		}
	}
	if (FAILED(result)) {
		goto release_objects;
	}
	result = frame->lpVtbl->Commit(frame);
	if (FAILED(result)) {
		goto release_objects;
	}
	result = encoder->lpVtbl->Commit(encoder);

release_objects:
	if (converter != NULL) {
		convertme_release_unknown((IUnknown *)converter);
	}
	if (bitmap != NULL) {
		convertme_release_unknown((IUnknown *)bitmap);
	}
	if (properties != NULL) {
		convertme_release_unknown((IUnknown *)properties);
	}
	if (frame != NULL) {
		convertme_release_unknown((IUnknown *)frame);
	}
	if (encoder != NULL) {
		convertme_release_unknown((IUnknown *)encoder);
	}
	if (stream != NULL) {
		convertme_release_unknown((IUnknown *)stream);
	}
	if (factory != NULL) {
		convertme_release_unknown((IUnknown *)factory);
	}

cleanup:
	if (shouldUninitialize) {
		CoUninitialize();
	}
	return result;
}
*/
import "C"

import (
	"fmt"
	"image"
	"runtime"
	"unsafe"
)

func heicCodecAvailable() bool {
	return C.convertme_heic_codec_available() == 1
}

func encodeHEICFile(path string, img image.Image, quality int) error {
	if !heicCodecAvailable() {
		return ErrHEICCodecMissing
	}

	bounds := img.Bounds()
	width, height := bounds.Dx(), bounds.Dy()
	pixels := make([]byte, width*height*4)
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			r, g, b, a := img.At(bounds.Min.X+x, bounds.Min.Y+y).RGBA()
			index := (y*width + x) * 4
			pixels[index] = byte(b >> 8)
			pixels[index+1] = byte(g >> 8)
			pixels[index+2] = byte(r >> 8)
			pixels[index+3] = byte(a >> 8)
		}
	}

	cPath := C.CString(path)
	defer C.free(unsafe.Pointer(cPath))
	result := C.convertme_encode_heic(
		(*C.BYTE)(unsafe.Pointer(&pixels[0])),
		C.UINT(width),
		C.UINT(height),
		cPath,
		C.UINT(quality),
	)
	runtime.KeepAlive(pixels)
	if result != 0 {
		if !heicCodecAvailable() {
			return ErrHEICCodecMissing
		}
		return fmt.Errorf("HEIC encoder returned HRESULT 0x%08x", uint32(result))
	}
	return nil
}
