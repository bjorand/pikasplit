#include <stdint.h>

// Starts capturing a display at w x h pixels and encoding it to H.264.
// Encoded frames are delivered to goScFrame, asynchronous errors to
// goScError, both tagged with handle. Returns NULL and sets *err (to free)
// on failure.
void *scStart(uint32_t displayID, int w, int h, int fps, int bitrate, uintptr_t handle, char **err);

// Asks for the next frame to be a keyframe, re-encoding the last captured
// image if the screen is idle.
void scRequestKeyframe(void *s);

// Stops capture and encoding; no callback runs after it returns.
void scStop(void *s);
