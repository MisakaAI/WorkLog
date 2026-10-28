#ifndef WORKLOG_NATIVE_H
#define WORKLOG_NATIVE_H

void worklog_run_menu(void);
void worklog_set_status(const char *status);
void worklog_set_login_state(int state);
void worklog_notify(const char *message, int failed);
void worklog_stop_menu(void);
int worklog_open_browser(const char *address);
void worklog_show_error(const char *message);
int worklog_has_acl(int fd);

#endif
