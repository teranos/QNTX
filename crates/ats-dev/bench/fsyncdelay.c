// 「[Sonnet 5.5] fsyncdelay: make every fsync/fdatasync cost FSYNC_US microseconds, and count them.」
//
//   「[Sonnet 5.5] cc -shared -fPIC -O2 -o fsyncdelay.so fsyncdelay.c -ldl」
//   「[Sonnet 5.5] LD_PRELOAD=$PWD/fsyncdelay.so FSYNC_US=8000 FSYNC_REPORT=1 ats-dev bench load 20000」
//
// 「[Sonnet 5.5] The data still reaches the real disk (the real fsync is called after the nap);」
// 「[Sonnet 5.5] the delay stands in for a slower disk. FSYNC_US unset or 0 adds no delay.」
// 「[Sonnet 5.5] FSYNC_REPORT=1 prints the totals to stderr at exit:」
//   「[Sonnet 5.5] fsyncdelay: fsync=<n> fdatasync=<n> delay_us=<n>」
#define _GNU_SOURCE
#include <dlfcn.h>
#include <errno.h>
#include <stdio.h>
#include <stdlib.h>
#include <time.h>
#include <unistd.h>

static long delay_us(void) {
    static long v = -1;
    if (v < 0) {
        const char *e = getenv("FSYNC_US");
        v = e ? atol(e) : 0;
    }
    return v;
}

static long n_fsync, n_fdatasync;

static void nap(void) {
    long u = delay_us();
    if (u > 0) {
        struct timespec t = { u / 1000000, (u % 1000000) * 1000 };
        nanosleep(&t, 0);
    }
}

int fsync(int fd) {
    static int (*real)(int);
    if (!real) real = dlsym(RTLD_NEXT, "fsync");
    if (!real) { errno = ENOSYS; return -1; }
    __atomic_fetch_add(&n_fsync, 1, __ATOMIC_RELAXED);
    nap();
    return real(fd);
}

int fdatasync(int fd) {
    static int (*real)(int);
    if (!real) real = dlsym(RTLD_NEXT, "fdatasync");
    if (!real) { errno = ENOSYS; return -1; }
    __atomic_fetch_add(&n_fdatasync, 1, __ATOMIC_RELAXED);
    nap();
    return real(fd);
}

__attribute__((destructor)) static void report(void) {
    const char *r = getenv("FSYNC_REPORT");
    if (r && *r == '1')
        fprintf(stderr, "fsyncdelay: fsync=%ld fdatasync=%ld delay_us=%ld\n",
                n_fsync, n_fdatasync, (n_fsync + n_fdatasync) * delay_us());
}
