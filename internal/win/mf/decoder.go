//go:build windows

package mf

import "errors"

// Image is a decoded NV12 picture. Pix holds the Y plane (Stride bytes per
// row, PlaneH rows) followed by the UV plane; Width x Height is the coded
// size, which may be padded beyond the visible picture.
type Image struct {
	Pix            []byte
	Width, Height  int
	Stride, PlaneH int
}

// Decoder decodes an Annex B H.264 stream to NV12 with the Microsoft H.264
// decoder MFT, in low-latency mode.
type Decoder struct {
	transform
	width, height, stride int
}

// NewDecoder creates a decoder. Call InitThread first.
func NewDecoder() (*Decoder, error) {
	t, err := createTransform(&clsidH264Decoder)
	if err != nil {
		return nil, err
	}
	d := &Decoder{transform: transform{t: t}}
	if err := d.init(); err != nil {
		d.Close()
		return nil, err
	}
	return d, nil
}

func (d *Decoder) init() error {
	var attrs uintptr
	if check("GetAttributes", vcall(d.t, vMFTGetAttributes, ptr(&attrs))) == nil {
		setUINT32(attrs, &mfLowLatency, 1)
		release(attrs)
	}
	in, err := newMediaType()
	if err != nil {
		return err
	}
	defer release(in)
	if err := setGUID(in, &mfMTMajorType, &mfMediaTypeVideo); err != nil {
		return err
	}
	if err := setGUID(in, &mfMTSubtype, &mfVideoFormatH264); err != nil {
		return err
	}
	if err := check("SetInputType", vcall(d.t, vMFTSetInputType, 0, in, 0)); err != nil {
		return err
	}
	if err := d.selectOutput(); err != nil {
		return err
	}
	if err := d.message(mftMessageNotifyBeginStreaming); err != nil {
		return err
	}
	return d.message(mftMessageNotifyStartOfStream)
}

// selectOutput picks NV12 among the output types the decoder offers. It is
// called again when the stream format changes (e.g. a new resolution).
func (d *Decoder) selectOutput() error {
	for i := 0; ; i++ {
		var t uintptr
		hr := vcall(d.t, vMFTGetOutputAvailableType, 0, uintptr(i), ptr(&t))
		if uint32(hr) == mfENoMoreTypes {
			return errors.New("mf: decoder offers no NV12 output")
		}
		if err := check("GetOutputAvailableType", hr); err != nil {
			return err
		}
		sub, _ := getGUID(t, &mfMTSubtype)
		if sub != mfVideoFormatNV12 {
			release(t)
			continue
		}
		err := check("SetOutputType", vcall(d.t, vMFTSetOutputType, 0, t, 0))
		if err == nil {
			if size, e := getUINT64(t, &mfMTFrameSize); e == nil {
				d.width, d.height = int(size>>32), int(uint32(size))
			}
			d.stride = d.width
			if s, e := getUINT32(t, &mfMTDefaultStride); e == nil && int32(s) > 0 {
				d.stride = int(int32(s))
			}
		}
		release(t)
		if err != nil {
			return err
		}
		return d.prepareOutput(d.stride * d.height * 3 / 2)
	}
}

// Decode feeds one access unit (ts in microseconds) and calls emit for each
// picture that comes out. img.Pix is only valid during the call.
func (d *Decoder) Decode(au []byte, ts int64, emit func(img Image)) error {
	sample, err := newSample(au, ts*10)
	if err != nil {
		return err
	}
	defer release(sample)
	for attempt := 0; ; attempt++ {
		accepted, err := d.input(sample)
		if err != nil {
			return err
		}
		if err := d.drain(emit); err != nil {
			return err
		}
		if accepted {
			return nil
		}
		if attempt > 0 {
			return errors.New("mf: decoder does not accept input")
		}
	}
}

func (d *Decoder) drain(emit func(Image)) error {
	for {
		ok, changed, err := d.output(func(b []byte) {
			if d.width == 0 || len(b) < d.stride*d.height*3/2 {
				return
			}
			emit(Image{Pix: b, Width: d.width, Height: d.height, Stride: d.stride, PlaneH: d.height})
		})
		switch {
		case err != nil:
			return err
		case changed:
			if err := d.selectOutput(); err != nil {
				return err
			}
		case !ok:
			return nil
		}
	}
}

// Reset drops buffered data, e.g. before resuming at a keyframe after
// frames were lost.
func (d *Decoder) Reset() error { return d.message(mftMessageCommandFlush) }

// Close releases the decoder.
func (d *Decoder) Close() { d.close() }
