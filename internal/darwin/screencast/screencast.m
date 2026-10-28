// Capture (ScreenCaptureKit) and hardware H.264 encoding (VideoToolbox).
// Compiled with ARC.

#import <CoreMedia/CoreMedia.h>
#import <Foundation/Foundation.h>
#import <ScreenCaptureKit/ScreenCaptureKit.h>
#import <VideoToolbox/VideoToolbox.h>

#include <stdlib.h>
#include <string.h>

#include "_cgo_export.h"
#include "screencast.h"

@interface PSStream : NSObject <SCStreamOutput, SCStreamDelegate>
@property(nonatomic) uintptr_t handle;
@property(nonatomic, strong) SCStream *stream;
@property(nonatomic, strong) dispatch_queue_t queue;
@property(nonatomic) VTCompressionSessionRef session;
@property(nonatomic) CVPixelBufferRef last; // retained manually
@property(atomic) BOOL forceKey;
@property(atomic) BOOL stopped;
@end

static void psEncoded(void *refcon, void *frameRefcon, OSStatus status, VTEncodeInfoFlags flags,
                      CMSampleBufferRef sb) {
	PSStream *s = (__bridge PSStream *)refcon;
	if (status != noErr || sb == NULL || s.stopped) return;

	BOOL key = YES;
	CFArrayRef atts = CMSampleBufferGetSampleAttachmentsArray(sb, false);
	if (atts != NULL && CFArrayGetCount(atts) > 0) {
		CFDictionaryRef d = CFArrayGetValueAtIndex(atts, 0);
		key = !CFDictionaryContainsKey(d, kCMSampleAttachmentKey_NotSync);
	}

	static const uint8_t startCode[4] = {0, 0, 0, 1};
	NSMutableData *out = [NSMutableData data];
	CMFormatDescriptionRef fmt = CMSampleBufferGetFormatDescription(sb);
	int nalLen = 4;
	size_t count = 0;
	CMVideoFormatDescriptionGetH264ParameterSetAtIndex(fmt, 0, NULL, NULL, &count, &nalLen);
	if (key) {
		// SPS and PPS in front of every keyframe, so a viewer can start there.
		for (size_t i = 0; i < count; i++) {
			const uint8_t *ps;
			size_t psLen;
			if (CMVideoFormatDescriptionGetH264ParameterSetAtIndex(fmt, i, &ps, &psLen, NULL, NULL) == noErr) {
				[out appendBytes:startCode length:4];
				[out appendBytes:ps length:psLen];
			}
		}
	}

	// AVCC (length-prefixed NAL units) -> Annex B (start codes).
	CMBlockBufferRef bb = CMSampleBufferGetDataBuffer(sb);
	size_t len = CMBlockBufferGetDataLength(bb);
	NSMutableData *raw = [NSMutableData dataWithLength:len];
	if (CMBlockBufferCopyDataBytes(bb, 0, len, raw.mutableBytes) != kCMBlockBufferNoErr) return;
	const uint8_t *r = raw.bytes;
	size_t off = 0;
	while (off + nalLen <= len) {
		size_t n = 0;
		for (int i = 0; i < nalLen; i++) n = (n << 8) | r[off + i];
		off += nalLen;
		if (n > len - off) break;
		[out appendBytes:startCode length:4];
		[out appendBytes:r + off length:n];
		off += n;
	}

	CMTime pts = CMSampleBufferGetPresentationTimeStamp(sb);
	int64_t us = CMTIME_IS_VALID(pts) ? (int64_t)(CMTimeGetSeconds(pts) * 1e6) : 0;
	goScFrame(s.handle, (uint8_t *)out.bytes, (int)out.length, us, key ? 1 : 0);
}

@implementation PSStream

- (void)encode:(CVPixelBufferRef)pb pts:(CMTime)pts {
	if (pb != self.last) {
		CVPixelBufferRetain(pb);
		if (self.last != NULL) CVPixelBufferRelease(self.last);
		self.last = pb;
	}
	NSDictionary *props = nil;
	if (self.forceKey) {
		self.forceKey = NO;
		props = @{(__bridge NSString *)kVTEncodeFrameOptionKey_ForceKeyFrame : @YES};
	}
	VTCompressionSessionEncodeFrame(self.session, pb, pts, kCMTimeInvalid, (__bridge CFDictionaryRef)props, NULL,
	                                NULL);
}

- (void)stream:(SCStream *)stream didOutputSampleBuffer:(CMSampleBufferRef)sb ofType:(SCStreamOutputType)type {
	if (type != SCStreamOutputTypeScreen || self.stopped) return;
	CFArrayRef atts = CMSampleBufferGetSampleAttachmentsArray(sb, false);
	if (atts == NULL || CFArrayGetCount(atts) == 0) return;
	NSDictionary *info = (__bridge NSDictionary *)CFArrayGetValueAtIndex(atts, 0);
	NSNumber *status = info[SCStreamFrameInfoStatus];
	// Idle frames (screen unchanged) carry no image.
	if (status == nil || status.integerValue != SCFrameStatusComplete) return;
	CVPixelBufferRef pb = CMSampleBufferGetImageBuffer(sb);
	if (pb == NULL) return;
	[self encode:pb pts:CMSampleBufferGetPresentationTimeStamp(sb)];
}

- (void)stream:(SCStream *)stream didStopWithError:(NSError *)error {
	if (self.stopped) return;
	goScError(self.handle, (char *)error.localizedDescription.UTF8String);
}

@end

static char *errString(NSString *msg) { return strdup(msg.UTF8String); }

static VTCompressionSessionRef newSession(PSStream *s, int w, int h, int fps, int bitrate) {
	VTCompressionSessionRef session = NULL;
	NSDictionary *spec = @{(__bridge NSString *)kVTVideoEncoderSpecification_EnableLowLatencyRateControl : @YES};
	OSStatus st = VTCompressionSessionCreate(NULL, w, h, kCMVideoCodecType_H264, (__bridge CFDictionaryRef)spec, NULL,
	                                         NULL, psEncoded, (__bridge void *)s, &session);
	if (st != noErr) {
		st = VTCompressionSessionCreate(NULL, w, h, kCMVideoCodecType_H264, NULL, NULL, NULL, psEncoded,
		                                (__bridge void *)s, &session);
	}
	if (st != noErr) return NULL;
	VTSessionSetProperty(session, kVTCompressionPropertyKey_RealTime, kCFBooleanTrue);
	VTSessionSetProperty(session, kVTCompressionPropertyKey_AllowFrameReordering, kCFBooleanFalse);
	VTSessionSetProperty(session, kVTCompressionPropertyKey_ProfileLevel, kVTProfileLevel_H264_High_AutoLevel);
	VTSessionSetProperty(session, kVTCompressionPropertyKey_AverageBitRate, (__bridge CFNumberRef) @(bitrate));
	VTSessionSetProperty(session, kVTCompressionPropertyKey_ExpectedFrameRate, (__bridge CFNumberRef) @(fps));
	VTSessionSetProperty(session, kVTCompressionPropertyKey_MaxKeyFrameInterval, (__bridge CFNumberRef) @(fps * 10));
	VTSessionSetProperty(session, kVTCompressionPropertyKey_ColorPrimaries, kCMFormatDescriptionColorPrimaries_ITU_R_709_2);
	VTSessionSetProperty(session, kVTCompressionPropertyKey_TransferFunction,
	                     kCMFormatDescriptionTransferFunction_ITU_R_709_2);
	VTSessionSetProperty(session, kVTCompressionPropertyKey_YCbCrMatrix, kCMFormatDescriptionYCbCrMatrix_ITU_R_709_2);
	VTCompressionSessionPrepareToEncodeFrames(session);
	return session;
}

static SCDisplay *findDisplay(uint32_t displayID, NSString **err) {
	// A display created a moment ago may take a little while to show up.
	for (int attempt = 0; attempt < 20; attempt++) {
		__block SCShareableContent *content = nil;
		__block NSError *cerr = nil;
		dispatch_semaphore_t sem = dispatch_semaphore_create(0);
		[SCShareableContent getShareableContentWithCompletionHandler:^(SCShareableContent *c, NSError *e) {
		  content = c;
		  cerr = e;
		  dispatch_semaphore_signal(sem);
		}];
		dispatch_semaphore_wait(sem, dispatch_time(DISPATCH_TIME_NOW, 5 * NSEC_PER_SEC));
		if (cerr != nil) {
			*err = cerr.localizedDescription;
			return nil;
		}
		for (SCDisplay *d in content.displays) {
			if (d.displayID == displayID) return d;
		}
		[NSThread sleepForTimeInterval:0.1];
	}
	*err = [NSString stringWithFormat:@"display %u not found by ScreenCaptureKit", displayID];
	return nil;
}

void *scStart(uint32_t displayID, int w, int h, int fps, int bitrate, uintptr_t handle, char **err) {
	@autoreleasepool {
		NSString *msg = nil;
		SCDisplay *display = findDisplay(displayID, &msg);
		if (display == nil) {
			*err = errString(msg);
			return NULL;
		}

		PSStream *s = [PSStream new];
		s.handle = handle;
		s.queue = dispatch_queue_create("pikasplit.screencast", DISPATCH_QUEUE_SERIAL);
		s.forceKey = YES;
		s.session = newSession(s, w, h, fps, bitrate);
		if (s.session == NULL) {
			*err = errString([NSString stringWithFormat:@"cannot create a %dx%d H.264 encoder", w, h]);
			return NULL;
		}

		SCContentFilter *filter = [[SCContentFilter alloc] initWithDisplay:display excludingWindows:@[]];
		SCStreamConfiguration *cfg = [SCStreamConfiguration new];
		cfg.width = w;
		cfg.height = h;
		cfg.minimumFrameInterval = CMTimeMake(1, fps);
		cfg.pixelFormat = kCVPixelFormatType_420YpCbCr8BiPlanarVideoRange;
		cfg.colorMatrix = kCGDisplayStreamYCbCrMatrix_ITU_R_709_2;
		cfg.showsCursor = YES;
		cfg.queueDepth = 5;

		s.stream = [[SCStream alloc] initWithFilter:filter configuration:cfg delegate:s];
		NSError *addErr = nil;
		if (![s.stream addStreamOutput:s type:SCStreamOutputTypeScreen sampleHandlerQueue:s.queue error:&addErr]) {
			*err = errString(addErr.localizedDescription);
			VTCompressionSessionInvalidate(s.session);
			CFRelease(s.session);
			return NULL;
		}
		__block NSError *startErr = nil;
		dispatch_semaphore_t sem = dispatch_semaphore_create(0);
		[s.stream startCaptureWithCompletionHandler:^(NSError *e) {
		  startErr = e;
		  dispatch_semaphore_signal(sem);
		}];
		dispatch_semaphore_wait(sem, dispatch_time(DISPATCH_TIME_NOW, 10 * NSEC_PER_SEC));
		if (startErr != nil) {
			*err = errString([NSString
			    stringWithFormat:@"%@ (allow Screen Recording for the app running pikasplit-host in System "
			                     @"Settings > Privacy & Security)",
			                     startErr.localizedDescription]);
			VTCompressionSessionInvalidate(s.session);
			CFRelease(s.session);
			return NULL;
		}
		return (void *)CFBridgingRetain(s);
	}
}

void scRequestKeyframe(void *p) {
	PSStream *s = (__bridge PSStream *)p;
	dispatch_async(s.queue, ^{
	  if (s.stopped) return;
	  s.forceKey = YES;
	  if (s.last != NULL) [s encode:s.last pts:CMClockGetTime(CMClockGetHostTimeClock())];
	});
}

void scStop(void *p) {
	@autoreleasepool {
		PSStream *s = (__bridge_transfer PSStream *)p;
		s.stopped = YES;
		dispatch_semaphore_t sem = dispatch_semaphore_create(0);
		[s.stream stopCaptureWithCompletionHandler:^(NSError *e) {
		  dispatch_semaphore_signal(sem);
		}];
		dispatch_semaphore_wait(sem, dispatch_time(DISPATCH_TIME_NOW, 5 * NSEC_PER_SEC));
		dispatch_sync(s.queue, ^{
		  VTCompressionSessionCompleteFrames(s.session, kCMTimeInvalid);
		  VTCompressionSessionInvalidate(s.session);
		  CFRelease(s.session);
		  s.session = NULL;
		  if (s.last != NULL) {
			  CVPixelBufferRelease(s.last);
			  s.last = NULL;
		  }
		});
		s.stream = nil;
	}
}
