//go:build windows

// Package mf wraps the Media Foundation H.264 decoder and encoder transforms
// (MFTs) through raw COM calls, without cgo.
package mf

import (
	"fmt"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"unsafe"
)

var (
	ole32  = syscall.NewLazyDLL("ole32.dll")
	mfplat = syscall.NewLazyDLL("mfplat.dll")

	pCoInitializeEx       = ole32.NewProc("CoInitializeEx")
	pCoCreateInstance     = ole32.NewProc("CoCreateInstance")
	pMFStartup            = mfplat.NewProc("MFStartup")
	pMFCreateMediaType    = mfplat.NewProc("MFCreateMediaType")
	pMFCreateSample       = mfplat.NewProc("MFCreateSample")
	pMFCreateMemoryBuffer = mfplat.NewProc("MFCreateMemoryBuffer")
)

func guid(s string) syscall.GUID {
	p := strings.Split(s, "-")
	u := func(s string, bits int) uint64 {
		v, err := strconv.ParseUint(s, 16, bits)
		if err != nil {
			panic(err)
		}
		return v
	}
	g := syscall.GUID{Data1: uint32(u(p[0], 32)), Data2: uint16(u(p[1], 16)), Data3: uint16(u(p[2], 16))}
	tail := p[3] + p[4]
	for i := range g.Data4 {
		g.Data4[i] = byte(u(tail[2*i:2*i+2], 8))
	}
	return g
}

var (
	clsidH264Decoder = guid("62CE7E72-4C71-4D20-B15D-452831A87D9D")
	clsidH264Encoder = guid("6CA50344-051A-4DED-9779-A43305165E35")
	iidIMFTransform  = guid("BF94C121-5B05-4E6F-8000-BA598961414D")
	iidICodecAPI     = guid("901DB4C7-31CE-41A2-85DC-8FA0BF41B8DA")

	mfMTMajorType     = guid("48EBA18E-F8C9-4687-BF11-0A74C9F96A8F")
	mfMTSubtype       = guid("F7E34C9A-42E8-4714-B74B-CB29D72C35E5")
	mfMTFrameSize     = guid("1652C33D-D6B2-4012-B834-72030849A37D")
	mfMTFrameRate     = guid("C459A2E8-3D2C-4E44-B132-FEE5156C7BB0")
	mfMTDefaultStride = guid("644B4E48-1E02-4516-B0EB-C01CA9D49AC6")
	mfMTAvgBitrate    = guid("20332624-FB0D-4D9E-BD0D-CBF6786C102E")
	mfMTInterlaceMode = guid("E2724BB8-E676-4806-B4B2-A8D6EFB44CCD")
	mfLowLatency      = guid("9C27891A-ED7A-40E1-88E8-B22727A024EE")

	mfMediaTypeVideo  = guid("73646976-0000-0010-8000-00AA00389B71")
	mfVideoFormatH264 = guid("34363248-0000-0010-8000-00AA00389B71")
	mfVideoFormatNV12 = guid("3231564E-0000-0010-8000-00AA00389B71")

	codecAPIForceKeyFrame = guid("398C1B98-8353-475A-9EF2-8F265D260345")
	codecAPIGOPSize       = guid("95F31B26-95A4-41AA-9303-246A7FC6EEF1")
	codecAPILowLatency    = guid("9C27891A-ED7A-40E1-88E8-B22727A024EE")
)

// HRESULTs and constants.
const (
	sOK                      = 0
	mfENotAccepting          = 0xC00D36B5
	mfENoMoreTypes           = 0xC00D36B9
	mfETransformNeedMore     = 0xC00D6D72
	mfETransformStreamChange = 0xC00D6D61

	mftMessageNotifyBeginStreaming = 0x10000000
	mftMessageNotifyStartOfStream  = 0x10000003
	mftMessageCommandFlush         = 0x00000000

	mftOutputStreamProvidesSamples = 0x100

	mfVideoInterlaceProgressive = 2
	clsctxInprocServer          = 1
	coinitMultithreaded         = 0
	mfVersion                   = 0x00020070
)

// IUnknown / IMFAttributes / IMFSample / IMFMediaBuffer / IMFTransform /
// ICodecAPI vtable indices.
const (
	vQueryInterface = 0
	vRelease        = 2

	vAttrGetUINT32 = 7
	vAttrGetUINT64 = 8
	vAttrGetGUID   = 10
	vAttrSetUINT32 = 21
	vAttrSetUINT64 = 22
	vAttrSetGUID   = 24

	vSampleSetSampleTime       = 36
	vSampleConvertToContiguous = 41
	vSampleAddBuffer           = 42

	vBufLock             = 3
	vBufUnlock           = 4
	vBufSetCurrentLength = 6

	vMFTGetOutputStreamInfo    = 7
	vMFTGetAttributes          = 8
	vMFTGetOutputAvailableType = 14
	vMFTSetInputType           = 15
	vMFTSetOutputType          = 16
	vMFTProcessMessage         = 23
	vMFTProcessInput           = 24
	vMFTProcessOutput          = 25

	vCodecSetValue = 9
)

// vcall calls method idx of the COM object this.
func vcall(this uintptr, idx int, args ...uintptr) uintptr {
	vtbl := **(***[64]uintptr)(unsafe.Pointer(&this))
	r, _, _ := syscall.SyscallN(vtbl[idx], append([]uintptr{this}, args...)...)
	return r
}

func release(obj uintptr) {
	if obj != 0 {
		vcall(obj, vRelease)
	}
}

func check(op string, hr uintptr) error {
	if int32(hr) < 0 {
		return fmt.Errorf("mf: %s: HRESULT 0x%08X", op, uint32(hr))
	}
	return nil
}

func ptr[T any](v *T) uintptr { return uintptr(unsafe.Pointer(v)) }

// bytesAt views n bytes of native memory at p.
func bytesAt(p uintptr, n int) []byte {
	return unsafe.Slice(*(**byte)(unsafe.Pointer(&p)), n)
}

var (
	startOnce sync.Once
	startErr  error
)

// InitThread prepares the calling OS thread for Media Foundation. Callers
// must keep using the transform from a goroutine locked to that thread.
func InitThread() error {
	pCoInitializeEx.Call(0, coinitMultithreaded)
	startOnce.Do(func() {
		r, _, _ := pMFStartup.Call(mfVersion, 0)
		startErr = check("MFStartup", r)
	})
	return startErr
}

func createTransform(clsid *syscall.GUID) (uintptr, error) {
	var t uintptr
	r, _, _ := pCoCreateInstance.Call(ptr(clsid), 0, clsctxInprocServer, ptr(&iidIMFTransform), ptr(&t))
	return t, check("CoCreateInstance", r)
}

func newMediaType() (uintptr, error) {
	var t uintptr
	r, _, _ := pMFCreateMediaType.Call(ptr(&t))
	return t, check("MFCreateMediaType", r)
}

func setGUID(attrs uintptr, key, val *syscall.GUID) error {
	return check("SetGUID", vcall(attrs, vAttrSetGUID, ptr(key), ptr(val)))
}

func setUINT32(attrs uintptr, key *syscall.GUID, v uint32) error {
	return check("SetUINT32", vcall(attrs, vAttrSetUINT32, ptr(key), uintptr(v)))
}

func setUINT64(attrs uintptr, key *syscall.GUID, v uint64) error {
	return check("SetUINT64", vcall(attrs, vAttrSetUINT64, ptr(key), uintptr(v)))
}

func getGUID(attrs uintptr, key *syscall.GUID) (syscall.GUID, error) {
	var g syscall.GUID
	return g, check("GetGUID", vcall(attrs, vAttrGetGUID, ptr(key), ptr(&g)))
}

func getUINT32(attrs uintptr, key *syscall.GUID) (uint32, error) {
	var v uint32
	return v, check("GetUINT32", vcall(attrs, vAttrGetUINT32, ptr(key), ptr(&v)))
}

func getUINT64(attrs uintptr, key *syscall.GUID) (uint64, error) {
	var v uint64
	return v, check("GetUINT64", vcall(attrs, vAttrGetUINT64, ptr(key), ptr(&v)))
}

// newSample wraps a copy of data in an IMFSample with time ts (100 ns).
func newSample(data []byte, ts int64) (uintptr, error) {
	buf, err := newBuffer(len(data))
	if err != nil {
		return 0, err
	}
	defer release(buf)
	var p, maxLen uintptr
	if err := check("Lock", vcall(buf, vBufLock, ptr(&p), ptr(&maxLen), 0)); err != nil {
		return 0, err
	}
	copy(bytesAt(p, len(data)), data)
	vcall(buf, vBufUnlock)
	if err := check("SetCurrentLength", vcall(buf, vBufSetCurrentLength, uintptr(len(data)))); err != nil {
		return 0, err
	}
	sample, err := newEmptySample(buf)
	if err != nil {
		return 0, err
	}
	vcall(sample, vSampleSetSampleTime, uintptr(ts))
	return sample, nil
}

func newBuffer(n int) (uintptr, error) {
	var buf uintptr
	r, _, _ := pMFCreateMemoryBuffer.Call(uintptr(n), ptr(&buf))
	return buf, check("MFCreateMemoryBuffer", r)
}

// newEmptySample returns a sample holding buf.
func newEmptySample(buf uintptr) (uintptr, error) {
	var s uintptr
	r, _, _ := pMFCreateSample.Call(ptr(&s))
	if err := check("MFCreateSample", r); err != nil {
		return 0, err
	}
	if err := check("AddBuffer", vcall(s, vSampleAddBuffer, buf)); err != nil {
		release(s)
		return 0, err
	}
	return s, nil
}

// withSampleBytes calls fn with the contiguous contents of sample.
func withSampleBytes(sample uintptr, fn func([]byte)) error {
	var buf uintptr
	if err := check("ConvertToContiguousBuffer", vcall(sample, vSampleConvertToContiguous, ptr(&buf))); err != nil {
		return err
	}
	defer release(buf)
	var p, maxLen, curLen uintptr
	if err := check("Lock", vcall(buf, vBufLock, ptr(&p), ptr(&maxLen), ptr(&curLen))); err != nil {
		return err
	}
	defer vcall(buf, vBufUnlock)
	fn(bytesAt(p, int(curLen)))
	return nil
}

type outputDataBuffer struct {
	streamID uint32
	sample   uintptr
	status   uint32
	events   uintptr
}

type outputStreamInfo struct {
	flags     uint32
	size      uint32
	alignment uint32
}

// transform is the part shared by the decoder and the encoder.
type transform struct {
	t               uintptr
	providesSamples bool
	outSize         int
	out             uintptr // caller-allocated output sample, if needed
	outBuf          uintptr // its buffer
}

func (m *transform) message(msg uint32) error {
	return check("ProcessMessage", vcall(m.t, vMFTProcessMessage, uintptr(msg), 0))
}

// prepareOutput (re)allocates the output sample after the output type was
// set. minSize is a lower bound for the output buffer.
func (m *transform) prepareOutput(minSize int) error {
	var info outputStreamInfo
	if err := check("GetOutputStreamInfo", vcall(m.t, vMFTGetOutputStreamInfo, 0, ptr(&info))); err != nil {
		return err
	}
	release(m.out)
	release(m.outBuf)
	m.out, m.outBuf = 0, 0
	m.providesSamples = info.flags&mftOutputStreamProvidesSamples != 0
	if m.providesSamples {
		return nil
	}
	m.outSize = max(int(info.size), minSize)
	buf, err := newBuffer(m.outSize)
	if err != nil {
		return err
	}
	m.out, err = newEmptySample(buf)
	if err != nil {
		release(buf)
		return err
	}
	m.outBuf = buf
	return nil
}

// input feeds a sample; it reports false when the transform is full and
// output must be drained first.
func (m *transform) input(sample uintptr) (accepted bool, err error) {
	hr := vcall(m.t, vMFTProcessInput, 0, sample, 0)
	if uint32(hr) == mfENotAccepting {
		return false, nil
	}
	return true, check("ProcessInput", hr)
}

// output pulls one output sample. ok is false when more input is needed;
// changed reports a stream format change (the caller renegotiates).
func (m *transform) output(fn func([]byte)) (ok, changed bool, err error) {
	ob := outputDataBuffer{sample: m.out}
	if m.outBuf != 0 {
		// The decoder fails (E_FAIL) on a reused buffer that still holds
		// the previous picture's length.
		vcall(m.outBuf, vBufSetCurrentLength, 0)
	}
	var status uint32
	hr := vcall(m.t, vMFTProcessOutput, 0, 1, ptr(&ob), ptr(&status))
	release(ob.events)
	switch uint32(hr) {
	case sOK:
		err := withSampleBytes(ob.sample, fn)
		if m.providesSamples {
			release(ob.sample)
		}
		return true, false, err
	case mfETransformNeedMore:
		return false, false, nil
	case mfETransformStreamChange:
		return false, true, nil
	}
	return false, false, check("ProcessOutput", hr)
}

func (m *transform) close() {
	release(m.out)
	release(m.outBuf)
	release(m.t)
	m.out, m.outBuf, m.t = 0, 0, 0
}
