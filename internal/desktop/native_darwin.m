//go:build darwin && cgo

#import <AppKit/AppKit.h>
#import <UniformTypeIdentifiers/UniformTypeIdentifiers.h>
#include <sys/acl.h>
#include <errno.h>
#include "native_darwin.h"

extern void worklogMenuCommand(int command);
extern void worklogBackupSelected(int importDB, const char *path);

@interface WorklogDelegate : NSObject <NSApplicationDelegate, NSMenuDelegate>
@property(strong) NSStatusItem *item;
@property(strong) NSMenuItem *status;
@property(strong) NSMenuItem *openItem;
@property(strong) NSMenuItem *restartItem;
@property(strong) NSMenuItem *quitItem;
@property(strong) NSMenuItem *loginItem;
@property(strong) NSMenuItem *importItem;
@property(strong) NSMenuItem *exportItem;
@property(strong) NSOpenPanel *panel;
@property(strong) NSAlert *alert;
@property(strong) NSMutableArray<NSDictionary *> *notices;
@property BOOL dialogBusy;
@property BOOL quitting;
@property BOOL terminationPending;
@end

static WorklogDelegate *host;

@implementation WorklogDelegate
- (void)applicationDidFinishLaunching:(NSNotification *)notification {
    self.item = [[NSStatusBar systemStatusBar] statusItemWithLength:NSVariableStatusItemLength];
    // Template artwork follows light/dark menu bars without bundled GUI assets.
    NSImage *image = [NSImage imageWithSystemSymbolName:@"checklist" accessibilityDescription:@"Worklog"];
    image.template = YES;
    self.item.button.image = image;
    if (!image) self.item.button.title = @"WL";
    self.item.button.toolTip = @"Worklog — 服务启动中";
    NSMenu *menu = [[NSMenu alloc] initWithTitle:@"Worklog"];
    menu.delegate = self;
    menu.autoenablesItems = NO;
    self.notices = [NSMutableArray new];
    self.openItem = [menu addItemWithTitle:@"打开 Worklog" action:@selector(openPage:) keyEquivalent:@""];
    self.openItem.target = self;
    [menu addItem:[NSMenuItem separatorItem]];
    self.status = [menu addItemWithTitle:@"● 服务启动中" action:nil keyEquivalent:@""];
    self.status.enabled = NO;
    [menu addItem:[NSMenuItem separatorItem]];
    self.importItem = [menu addItemWithTitle:@"导入备份…" action:@selector(importBackup:) keyEquivalent:@""];
    self.importItem.target = self;
    self.exportItem = [menu addItemWithTitle:@"导出备份…" action:@selector(exportBackup:) keyEquivalent:@""];
    self.exportItem.target = self;
    [menu addItem:[NSMenuItem separatorItem]];
    self.loginItem = [menu addItemWithTitle:@"登录后启动" action:@selector(toggleLogin:) keyEquivalent:@""];
    self.loginItem.target = self;
    [menu addItem:[NSMenuItem separatorItem]];
    self.restartItem = [menu addItemWithTitle:@"重启服务" action:@selector(restart:) keyEquivalent:@""];
    self.restartItem.target = self;
    self.quitItem = [menu addItemWithTitle:@"退出 Worklog" action:@selector(quit:) keyEquivalent:@""];
    self.quitItem.target = self;
    self.item.menu = menu;
    worklogMenuCommand(0);
}
- (void)menuWillOpen:(NSMenu *)menu { if (!self.quitting) worklogMenuCommand(5); }
- (void)openPage:(id)sender { if (!self.quitting) worklogMenuCommand(1); }
- (void)restart:(id)sender { if (!self.quitting) worklogMenuCommand(2); }
- (void)toggleLogin:(id)sender { if (!self.quitting) worklogMenuCommand(4); }
- (void)importBackup:(id)sender { [self requestBackup:YES]; }
- (void)exportBackup:(id)sender { [self requestBackup:NO]; }
- (void)updateBackupItems {
    self.importItem.enabled = self.exportItem.enabled = !self.quitting && !self.dialogBusy;
}
- (void)requestBackup:(BOOL)importDB {
    if (self.quitting || self.dialogBusy) return;
    self.dialogBusy = YES;
    [self updateBackupItems];
    NSOpenPanel *panel = [NSOpenPanel openPanel];
    self.panel = panel;
    panel.title = importDB ? @"选择要导入的备份数据库" : @"选择导出备份的目录";
    panel.prompt = importDB ? @"选择备份" : @"选择目录";
    panel.canChooseFiles = importDB;
    panel.canChooseDirectories = !importDB;
    panel.allowsMultipleSelection = NO;
    panel.canCreateDirectories = !importDB;
    if (importDB) panel.allowedContentTypes = @[[UTType typeWithFilenameExtension:@"db"] ?: UTTypeData];
    [NSApp activateIgnoringOtherApps:YES];
    [panel beginWithCompletionHandler:^(NSModalResponse response) {
        NSString *path = response == NSModalResponseOK ? panel.URL.path : nil;
        self.panel = nil;
        if (path && importDB && !self.quitting) {
            NSAlert *alert = [NSAlert new];
            self.alert = alert;
            alert.messageText = @"导入备份";
            alert.informativeText = @"导入将切换当前数据库，原数据库会保留。请先保存页面编辑，停止其他 HTTP／stdio 写入程序，并选择不再使用中的独立备份。确定继续吗？";
            alert.alertStyle = NSAlertStyleWarning;
            [alert addButtonWithTitle:@"取消"];
            [alert addButtonWithTitle:@"导入"];
            if ([alert runModal] != NSAlertSecondButtonReturn) path = nil;
            self.alert = nil;
        }
        self.dialogBusy = NO;
        [self updateBackupItems];
        if (path && !self.quitting) worklogBackupSelected(importDB ? 1 : 0, path.UTF8String);
        [self showNextNotice];
    }];
}
- (void)showNextNotice {
    if (self.quitting || self.dialogBusy || self.notices.count == 0) return;
    NSDictionary *notice = self.notices.firstObject;
    [self.notices removeObjectAtIndex:0];
    self.dialogBusy = YES;
    [self updateBackupItems];
    NSAlert *alert = [NSAlert new];
    self.alert = alert;
    alert.messageText = [notice[@"failed"] boolValue] ? @"Worklog 操作失败" : @"Worklog";
    alert.informativeText = notice[@"message"];
    alert.alertStyle = [notice[@"failed"] boolValue] ? NSAlertStyleWarning : NSAlertStyleInformational;
    [NSApp activateIgnoringOtherApps:YES];
    [alert runModal];
    self.alert = nil;
    self.dialogBusy = NO;
    [self updateBackupItems];
    dispatch_async(dispatch_get_main_queue(), ^{ [self showNextNotice]; });
}
- (void)prepareToQuit {
    self.quitting = YES;
    self.openItem.enabled = self.restartItem.enabled = self.quitItem.enabled = NO;
    self.loginItem.enabled = NO;
    [self updateBackupItems];
    [self.notices removeAllObjects];
    [self.panel cancel:nil];
    if (self.alert) { [NSApp abortModal]; [self.alert.window orderOut:nil]; }
    self.status.title = @"● 服务停止中";
}
- (void)quit:(id)sender {
    if (self.quitting) return;
    [self prepareToQuit];
    worklogMenuCommand(3);
}
- (BOOL)applicationShouldHandleReopen:(NSApplication *)sender hasVisibleWindows:(BOOL)visible {
    [self openPage:nil]; // Finder/LaunchServices may reopen without spawning a process.
    return NO;
}
- (NSApplicationTerminateReply)applicationShouldTerminate:(NSApplication *)sender {
    self.terminationPending = YES;
    [self quit:nil];
    return NSTerminateLater;
}
@end

void worklog_run_menu(void) {
    @autoreleasepool {
        [NSApplication sharedApplication];
        [NSApp setActivationPolicy:NSApplicationActivationPolicyAccessory];
        host = [WorklogDelegate new];
        NSApp.delegate = host;
        [NSApp run];
        [[NSStatusBar systemStatusBar] removeStatusItem:host.item];
        NSApp.delegate = nil;
        host = nil;
    }
}

void worklog_set_status(const char *status) {
    @autoreleasepool {
        NSString *text = [NSString stringWithUTF8String:status];
        dispatch_async(dispatch_get_main_queue(), ^{
            if (!host.quitting) host.status.title = text;
            host.item.button.toolTip = [@"Worklog — " stringByAppendingString:text];
        });
    }
}

void worklog_stop_menu(void) {
    dispatch_async(dispatch_get_main_queue(), ^{
        [host prepareToQuit];
        // Service/database cleanup is complete before this is called.
        if (host.terminationPending) [NSApp replyToApplicationShouldTerminate:YES];
        [NSApp stop:nil];
        // stop: alone does not wake nextEventMatchingMask in an idle run loop.
        [NSApp postEvent:[NSEvent otherEventWithType:NSEventTypeApplicationDefined
            location:NSZeroPoint modifierFlags:0 timestamp:0 windowNumber:0
            context:nil subtype:0 data1:0 data2:0] atStart:YES];
    });
}

void worklog_set_login_state(int state) {
    // Called synchronously from the Go menu callbacks, on the AppKit thread.
    host.loginItem.state = state > 0 ? NSControlStateValueOn : NSControlStateValueOff;
    host.loginItem.title = state < 0 ? @"登录后启动（状态读取失败）" : @"登录后启动";
}

void worklog_notify(const char *message, int failed) {
    @autoreleasepool {
        NSString *text = [NSString stringWithUTF8String:message];
        dispatch_async(dispatch_get_main_queue(), ^{
            if (!host || host.quitting) return;
            [host.notices addObject:@{@"message": text, @"failed": @(failed != 0)}];
            [host showNextNotice];
        });
    }
}

int worklog_open_browser(const char *address) {
    @autoreleasepool {
        NSURL *url = [NSURL URLWithString:[NSString stringWithUTF8String:address]];
        // NSWorkspace launches the registered browser; no shell or WebView.
        return url && [[NSWorkspace sharedWorkspace] openURL:url];
    }
}

void worklog_show_error(const char *message) {
    @autoreleasepool {
        [NSApplication sharedApplication];
        [NSApp setActivationPolicy:NSApplicationActivationPolicyAccessory];
        NSAlert *alert = [NSAlert new];
        alert.messageText = @"Worklog 无法启动";
        alert.informativeText = [NSString stringWithUTF8String:message];
        alert.alertStyle = NSAlertStyleCritical;
        [NSApp activateIgnoringOtherApps:YES];
        [alert runModal];
    }
}

int worklog_has_acl(int fd) {
    acl_t acl = acl_get_fd_np(fd, ACL_TYPE_EXTENDED);
    // On Darwin a valid descriptor without an extended ACL can report ENOENT.
    // The descriptor has already been validated with fstat by the caller.
    if (!acl) return errno == ENOENT ? 0 : -1;
    acl_entry_t entry;
    int result = acl_get_entry(acl, ACL_FIRST_ENTRY, &entry);
    int saved = errno;
    acl_free(acl);
    // macOS returns -1/EINVAL when the ACL has no entries.
    if (result == -1 && saved == EINVAL) return 0;
    errno = saved;
    return result == 0 ? 1 : -1;
}
