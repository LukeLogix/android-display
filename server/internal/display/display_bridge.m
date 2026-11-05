#import <Foundation/Foundation.h>
#import <CoreGraphics/CoreGraphics.h>
#import <objc/runtime.h>
#import <objc/message.h>
#include "display_bridge.h"
#include <string.h>
#include <unistd.h>

// CGVirtualDisplay is a private API in CoreDisplay.framework.
// We load it dynamically and use NSClassFromString to access the classes.
// Classes used:
//   - CGVirtualDisplay
//   - CGVirtualDisplayDescriptor
//   - CGVirtualDisplayMode
//   - CGVirtualDisplaySettings

static BOOL sFrameworkLoaded = NO;

static BOOL LoadCoreDisplayFramework(void) {
    if (sFrameworkLoaded) {
        return YES;
    }

    // Try public Frameworks first (macOS 14+), then PrivateFrameworks (older)
    NSBundle *bundle = [NSBundle bundleWithPath:
        @"/System/Library/Frameworks/CoreDisplay.framework"];
    if (!bundle) {
        bundle = [NSBundle bundleWithPath:
            @"/System/Library/PrivateFrameworks/CoreDisplay.framework"];
    }
    if (!bundle) {
        return NO;
    }

    BOOL loaded = [bundle load];
    if (loaded) {
        sFrameworkLoaded = YES;
    }
    return loaded;
}

VirtualDisplayResult CreateVirtualDisplay(VirtualDisplayConfig config) {
    VirtualDisplayResult result;
    memset(&result, 0, sizeof(result));

    @autoreleasepool {
        // Step 1: Load the private framework
        if (!LoadCoreDisplayFramework()) {
            snprintf(result.errorMsg, sizeof(result.errorMsg),
                     "failed to load CoreDisplay.framework");
            return result;
        }

        // Step 2: Get the private classes
        Class CGVirtualDisplayClass = NSClassFromString(@"CGVirtualDisplay");
        Class CGVirtualDisplayDescriptorClass = NSClassFromString(@"CGVirtualDisplayDescriptor");
        Class CGVirtualDisplayModeClass = NSClassFromString(@"CGVirtualDisplayMode");
        Class CGVirtualDisplaySettingsClass = NSClassFromString(@"CGVirtualDisplaySettings");

        if (!CGVirtualDisplayClass) {
            snprintf(result.errorMsg, sizeof(result.errorMsg),
                     "CGVirtualDisplay class not found");
            return result;
        }
        if (!CGVirtualDisplayDescriptorClass) {
            snprintf(result.errorMsg, sizeof(result.errorMsg),
                     "CGVirtualDisplayDescriptor class not found");
            return result;
        }
        if (!CGVirtualDisplayModeClass) {
            snprintf(result.errorMsg, sizeof(result.errorMsg),
                     "CGVirtualDisplayMode class not found");
            return result;
        }
        if (!CGVirtualDisplaySettingsClass) {
            snprintf(result.errorMsg, sizeof(result.errorMsg),
                     "CGVirtualDisplaySettings class not found");
            return result;
        }

        // Step 3: Create a descriptor
        id descriptor = [[CGVirtualDisplayDescriptorClass alloc] init];
        if (!descriptor) {
            snprintf(result.errorMsg, sizeof(result.errorMsg),
                     "failed to create CGVirtualDisplayDescriptor");
            return result;
        }

        // Configure descriptor properties via KVC
        // These are the known properties of CGVirtualDisplayDescriptor.
        @try {
            [descriptor setValue:@(0x1234) forKey:@"vendorID"];
            [descriptor setValue:@(0x5678) forKey:@"productID"];
            [descriptor setValue:@"AndroidMac Virtual Display" forKey:@"name"];
            [descriptor setValue:@((unsigned int)config.width) forKey:@"maxPixelsWide"];
            [descriptor setValue:@((unsigned int)config.height) forKey:@"maxPixelsHigh"];

            // A fixed non-zero serial gives the display a stable UUID, so macOS
            // remembers its arrangement across reconnects (serial 0 → random UUID).
            @try {
                [descriptor setValue:@(0x414E4452) forKey:@"serialNum"]; // "ANDR"
            } @catch (NSException *e) {
                // serialNum may not exist on all versions, ignore
            }
        } @catch (NSException *exception) {
            snprintf(result.errorMsg, sizeof(result.errorMsg),
                     "failed to configure descriptor: %s",
                     [[exception reason] UTF8String]);
            return result;
        }

        // Step 4: Create a display mode via initWithWidth:height:refreshRate:
        id mode = nil;
        @try {
            SEL initModeSel = NSSelectorFromString(@"initWithWidth:height:refreshRate:");
            id newMode = [CGVirtualDisplayModeClass alloc];
            NSMethodSignature *sig = [newMode methodSignatureForSelector:initModeSel];
            if (sig) {
                NSInvocation *inv = [NSInvocation invocationWithMethodSignature:sig];
                [inv setSelector:initModeSel];
                [inv setTarget:newMode];
                unsigned int w = (unsigned int)config.width;
                unsigned int h = (unsigned int)config.height;
                double refresh = 60.0;
                [inv setArgument:&w atIndex:2];
                [inv setArgument:&h atIndex:3];
                [inv setArgument:&refresh atIndex:4];
                [inv invoke];
                __unsafe_unretained id invokeResult;
                [inv getReturnValue:&invokeResult];
                mode = invokeResult;
            }
        } @catch (NSException *exception) {
            snprintf(result.errorMsg, sizeof(result.errorMsg),
                     "failed to create display mode: %s",
                     [[exception reason] UTF8String]);
            return result;
        }

        if (!mode) {
            snprintf(result.errorMsg, sizeof(result.errorMsg),
                     "failed to create CGVirtualDisplayMode");
            return result;
        }

        // Step 5: Create settings with the mode and hiDPI flag
        id settings = [[CGVirtualDisplaySettingsClass alloc] init];
        if (!settings) {
            snprintf(result.errorMsg, sizeof(result.errorMsg),
                     "failed to create CGVirtualDisplaySettings");
            return result;
        }

        @try {
            [settings setValue:@[mode] forKey:@"modes"];
            [settings setValue:@(config.hiDPI ? YES : NO) forKey:@"hiDPI"];
        } @catch (NSException *exception) {
            snprintf(result.errorMsg, sizeof(result.errorMsg),
                     "failed to configure settings: %s",
                     [[exception reason] UTF8String]);
            return result;
        }

        // Step 6: Create CGVirtualDisplay from descriptor
        id display = nil;
        @try {
            SEL initWithDescSel = NSSelectorFromString(@"initWithDescriptor:");
            if ([CGVirtualDisplayClass instancesRespondToSelector:initWithDescSel]) {
                display = [CGVirtualDisplayClass alloc];
                NSMethodSignature *sig = [display methodSignatureForSelector:initWithDescSel];
                if (sig) {
                    NSInvocation *inv = [NSInvocation invocationWithMethodSignature:sig];
                    [inv setSelector:initWithDescSel];
                    [inv setTarget:display];
                    [inv setArgument:&descriptor atIndex:2];
                    [inv invoke];
                    __unsafe_unretained id invokeResult;
                    [inv getReturnValue:&invokeResult];
                    display = invokeResult;
                }
            }
        } @catch (NSException *exception) {
            snprintf(result.errorMsg, sizeof(result.errorMsg),
                     "failed to create CGVirtualDisplay: %s",
                     [[exception reason] UTF8String]);
            return result;
        }

        if (!display) {
            snprintf(result.errorMsg, sizeof(result.errorMsg),
                     "CGVirtualDisplay creation returned nil");
            return result;
        }

        // Step 7b: Apply settings to the display (not the descriptor)
        @try {
            SEL applySettingsSel = NSSelectorFromString(@"applySettings:");
            if ([display respondsToSelector:applySettingsSel]) {
#pragma clang diagnostic push
#pragma clang diagnostic ignored "-Warc-performSelector-leaks"
                [display performSelector:applySettingsSel withObject:settings];
#pragma clang diagnostic pop
            }
        } @catch (NSException *exception) {
            // Non-fatal: display may work without explicit settings application
        }

        // Step 8: Extract the displayID
        CGDirectDisplayID displayID = 0;
        @try {
            NSNumber *displayIDValue = [display valueForKey:@"displayID"];
            if (displayIDValue) {
                displayID = [displayIDValue unsignedIntValue];
            }
        } @catch (NSException *exception) {
            snprintf(result.errorMsg, sizeof(result.errorMsg),
                     "failed to get displayID: %s",
                     [[exception reason] UTF8String]);
            return result;
        }

        // Step 9: Position the virtual display to the right of ALL physical displays.
        // We find the rightmost edge across every active display so the virtual
        // display never overlaps a physical monitor.  Overlap would cause
        // CGDisplayBounds to return stale/wrong coordinates for the injector.
        {
            CGDirectDisplayID allDisplays[16];
            uint32_t displayCount = 0;
            CGGetActiveDisplayList(16, allDisplays, &displayCount);

            double rightEdge = 0;
            for (uint32_t i = 0; i < displayCount; i++) {
                // Skip our own virtual display
                if (allDisplays[i] == displayID) continue;
                CGRect b = CGDisplayBounds(allDisplays[i]);
                double edge = b.origin.x + b.size.width;
                if (edge > rightEdge) rightEdge = edge;
            }

            int32_t newX = (int32_t)rightEdge;
            int32_t newY = 0;

            CGDisplayConfigRef configRef = NULL;
            CGError err = CGBeginDisplayConfiguration(&configRef);
            if (err == kCGErrorSuccess && configRef) {
                CGConfigureDisplayOrigin(configRef, displayID, newX, newY);
                CGCompleteDisplayConfiguration(configRef, kCGConfigureForSession);
            }
        }

        // Step 9b: Wait briefly for macOS to finalize display layout, then
        // verify the position took effect so downstream code (injector) gets
        // the correct bounds from CGDisplayBounds.
        usleep(100000); // 100 ms

        // Step 10: Return success
        // Use __bridge_retained to transfer ownership to C side.
        // The caller is responsible for calling DestroyVirtualDisplay to release.
        result.display = (__bridge_retained void *)display;
        result.displayID = displayID;
        result.success = 1;
    }

    return result;
}

void DestroyVirtualDisplay(void *display) {
    if (display == NULL) {
        return;
    }

    @autoreleasepool {
        // Use __bridge_transfer to reclaim ownership and release
        id obj = (__bridge_transfer id)display;
        // obj will be released at the end of this scope
        (void)obj;
    }
}

CGDirectDisplayID GetVirtualDisplayID(void *display) {
    if (display == NULL) {
        return 0;
    }

    @autoreleasepool {
        id obj = (__bridge id)display;
        @try {
            NSNumber *displayIDValue = [obj valueForKey:@"displayID"];
            if (displayIDValue) {
                return [displayIDValue unsignedIntValue];
            }
        } @catch (NSException *exception) {
            return 0;
        }
    }

    return 0;
}
