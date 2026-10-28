//go:build windows

package mf

import (
	"errors"
	"syscall"
)

// Encoder encodes NV12 pictures to an Annex B H.264 stream with the
// Microsoft H.264 encoder MFT. It is used for development (a video source
// without a Mac) and tests.
type Encoder struct {
	transform
	codec              uintptr // ICodecAPI, may be 0
	width, height, fps int
}

const (
	vtUI4         = 19
	vtBool        = 11
	variantTrue   = 0xFFFF
	vSampleSetDur = 38
)

type variant struct {
	vt  uint16
	_   [3]uint16
	val uint64
	_   uint64
}

// NewEncoder creates an encoder for w x h pictures (even sizes). Call
// InitThread first.
func NewEncoder(w, h, fps, bitrate int) (*Encoder, error) {
	t, err := createTransform(&clsidH264Encoder)
	if err != nil {
		return nil, err
	}
	e := &Encoder{transform: transform{t: t}, width: w, height: h, fps: fps}
	if err := e.init(bitrate); err != nil {
		e.Close()
		return nil, err
	}
	return e, nil
}

func (e *Encoder) init(bitrate int) error {
	size := uint64(e.width)<<32 | uint64(e.height)
	rate := uint64(e.fps)<<32 | 1
	setType := func(subtype uintptr, method int, extra func(t uintptr) error) error {
		t, err := newMediaType()
		if err != nil {
			return err
		}
		defer release(t)
		for _, err := range []error{
			setGUID(t, &mfMTMajorType, &mfMediaTypeVideo),
			check("SetGUID", vcall(t, vAttrSetGUID, ptr(&mfMTSubtype), subtype)),
			setUINT64(t, &mfMTFrameSize, size),
			setUINT64(t, &mfMTFrameRate, rate),
			setUINT32(t, &mfMTInterlaceMode, mfVideoInterlaceProgressive),
		} {
			if err != nil {
				return err
			}
		}
		if extra != nil {
			if err := extra(t); err != nil {
				return err
			}
		}
		return check("SetType", vcall(e.t, method, 0, t, 0))
	}
	// The encoder wants its output type before its input type.
	if err := setType(ptr(&mfVideoFormatH264), vMFTSetOutputType, func(t uintptr) error {
		return setUINT32(t, &mfMTAvgBitrate, uint32(bitrate))
	}); err != nil {
		return err
	}
	if err := setType(ptr(&mfVideoFormatNV12), vMFTSetInputType, nil); err != nil {
		return err
	}

	if check("QueryInterface", vcall(e.t, vQueryInterface, ptr(&iidICodecAPI), ptr(&e.codec))) == nil {
		e.setCodec(&codecAPILowLatency, variant{vt: vtBool, val: variantTrue})
		e.setCodec(&codecAPIGOPSize, variant{vt: vtUI4, val: uint64(e.fps * 2)})
	}
	if err := e.prepareOutput(e.width * e.height * 3 / 2); err != nil {
		return err
	}
	if err := e.message(mftMessageNotifyBeginStreaming); err != nil {
		return err
	}
	return e.message(mftMessageNotifyStartOfStream)
}

func (e *Encoder) setCodec(key *syscall.GUID, v variant) {
	if e.codec != 0 {
		vcall(e.codec, vCodecSetValue, ptr(key), ptr(&v))
	}
}

// RequestKeyframe makes the next picture a keyframe, when supported.
func (e *Encoder) RequestKeyframe() {
	e.setCodec(&codecAPIForceKeyFrame, variant{vt: vtUI4, val: 1})
}

// Encode feeds one NV12 picture (ts in microseconds) and calls emit for each
// access unit that comes out. au is only valid during the call.
func (e *Encoder) Encode(nv12 []byte, ts int64, emit func(au []byte)) error {
	if len(nv12) < e.width*e.height*3/2 {
		return errors.New("mf: short NV12 picture")
	}
	sample, err := newSample(nv12, ts*10)
	if err != nil {
		return err
	}
	defer release(sample)
	vcall(sample, vSampleSetDur, uintptr(10_000_000/e.fps))
	for attempt := 0; ; attempt++ {
		accepted, err := e.input(sample)
		if err != nil {
			return err
		}
		for {
			ok, _, err := e.output(emit)
			if err != nil {
				return err
			}
			if !ok {
				break
			}
		}
		if accepted {
			return nil
		}
		if attempt > 0 {
			return errors.New("mf: encoder does not accept input")
		}
	}
}

// Close releases the encoder.
func (e *Encoder) Close() {
	release(e.codec)
	e.codec = 0
	e.close()
}
