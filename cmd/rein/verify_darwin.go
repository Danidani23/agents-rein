//go:build app && darwin

package main

/*
#cgo CFLAGS: -x objective-c -fobjc-arc
#cgo LDFLAGS: -framework LocalAuthentication -framework Foundation
#import <LocalAuthentication/LocalAuthentication.h>
#include <stdlib.h>
#include <string.h>

// reinVerifyOwner asks the device's owner, by Touch ID, an Apple Watch or
// the password, and waits for the answer: NULL when they passed, else what
// went wrong, which the caller frees.
static char *reinVerifyOwner(const char *reason) {
	LAContext *context = [[LAContext alloc] init];
	NSError *availability = nil;
	if (![context canEvaluatePolicy:LAPolicyDeviceOwnerAuthentication error:&availability]) {
		return strdup(availability.localizedDescription.UTF8String);
	}
	__block char *failure = NULL;
	dispatch_semaphore_t answered = dispatch_semaphore_create(0);
	[context evaluatePolicy:LAPolicyDeviceOwnerAuthentication localizedReason:[NSString stringWithUTF8String:reason] reply:^(BOOL passed, NSError *refused) {
		if (!passed) {
			failure = strdup(refused.localizedDescription.UTF8String);
		}
		dispatch_semaphore_signal(answered);
	}];
	dispatch_semaphore_wait(answered, DISPATCH_TIME_FOREVER);
	return failure;
}
*/
import "C"

import (
	"errors"
	"unsafe"
)

// verifyOwner is macOS's own check of the developer before a Confirm in
// the window counts (P6.8): Touch ID, an Apple Watch or the password, as
// the policy of the device's owner allows
// (https://developer.apple.com/documentation/localauthentication/lapolicy/deviceownerauthentication).
var verifyOwner = func(reason string) error {
	text := C.CString(reason)
	defer C.free(unsafe.Pointer(text))
	failure := C.reinVerifyOwner(text)
	if failure == nil {
		return nil
	}
	defer C.free(unsafe.Pointer(failure))
	return errors.New(C.GoString(failure))
}
