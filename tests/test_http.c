// SPDX-License-Identifier: GPL-3.0-or-later
// The installer's minimal HTTP: server and client.
#if !defined(_WIN32)
#define _POSIX_C_SOURCE 200809L
#endif
#include <stdlib.h>

#include "mp_http.h"
#include "mp_platform.h"
#include "mp_test.h"

#ifdef _WIN32
#include <winsock2.h>
#include <ws2tcpip.h>
typedef SOCKET sock_t;
#define close_sock closesocket
#else
#include <arpa/inet.h>
#include <netinet/in.h>
#include <sys/socket.h>
#include <unistd.h>
typedef int sock_t;
#define close_sock close
#endif

static void echo(void *ctx, const mp_http_req *rq, mp_http_resp *rs) {
    (void)ctx;
    size_t n = strlen(rq->method) + strlen(rq->path) + strlen(rq->query) + rq->body_len + 4;
    rs->body = malloc(n);
    int k = snprintf(rs->body, n, "%s\n%s\n%s\n", rq->method, rq->path, rq->query);
    memcpy(rs->body + k, rq->body, rq->body_len);
    rs->body_len = (size_t)k + rq->body_len;
    rs->status = 200;
    rs->content_type = "text/plain";
}

static sock_t connect_to(uint16_t port) {
    sock_t s = socket(AF_INET, SOCK_STREAM, 0);
    struct sockaddr_in a;
    memset(&a, 0, sizeof a);
    a.sin_family = AF_INET;
    a.sin_port = htons(port);
    inet_pton(AF_INET, "127.0.0.1", &a.sin_addr);
    connect(s, (struct sockaddr *)&a, sizeof a);
    return s;
}

// Sends raw as is and returns the response's status code (or -1).
static int raw_status(uint16_t port, const char *raw) {
    sock_t s = connect_to(port);
    send(s, raw, (int)strlen(raw), 0);
    char buf[256];
    int n = (int)recv(s, buf, sizeof buf - 1, 0);
    close_sock(s);
    if (n <= 12) return -1;
    buf[n] = 0;
    int code = -1;
    if (!strncmp(buf, "HTTP/1.1 ", 9)) code = atoi(buf + 9);
    return code;
}

MP_TEST(http_roundtrip) {
    mp_err e = {{0}};
    mp_http_server *s = mp_http_start("127.0.0.1", 0, echo, NULL, &e);
    MP_CHECK(s != NULL);
    if (!s) return;
    uint16_t port = mp_http_port(s);
    MP_CHECK(port != 0);
    size_t bl = 10000;
    char *body = malloc(bl);
    for (size_t i = 0; i < bl; i++) body[i] = (char)('A' + i % 26);
    int st = 0;
    char *resp = NULL;
    size_t rl = 0;
    MP_CHECK(mp_http_request("127.0.0.1", port, "POST", "/api/x?a=1", body, bl, 3000, &st, &resp, &rl, &e) == 0);
    if (e.msg[0]) fprintf(stderr, "  %s\n", e.msg);
    MP_CHECK(st == 200);
    const char *head = "POST\n/api/x\na=1\n";
    MP_CHECK(resp && rl == strlen(head) + bl && !memcmp(resp, head, strlen(head)) &&
             !memcmp(resp + strlen(head), body, bl));
    free(resp);
    MP_CHECK(mp_http_request("127.0.0.1", port, "GET", "/", NULL, 0, 3000, &st, &resp, &rl, &e) == 0);
    MP_CHECK(st == 200 && resp && !strncmp(resp, "GET\n/\n\n", 7));
    free(resp);
    free(body);
    mp_http_stop(s);
}

MP_TEST(http_limits) {
    mp_err e = {{0}};
    mp_http_server *s = mp_http_start("127.0.0.1", 0, echo, NULL, &e);
    MP_CHECK(s != NULL);
    if (!s) return;
    uint16_t port = mp_http_port(s);
    MP_CHECK(raw_status(port, "POST /x HTTP/1.1\r\nContent-Length: 2097152\r\n\r\n") == 413);
    MP_CHECK(raw_status(port, "GET /\r\n\r\n") == 400);
    int all = 1;
    for (int i = 0; i < 50; i++) {
        int st = 0;
        char *resp = NULL;
        size_t rl = 0;
        if (mp_http_request("127.0.0.1", port, "GET", "/n", NULL, 0, 3000, &st, &resp, &rl, &e) != 0 || st != 200)
            all = 0;
        free(resp);
    }
    MP_CHECK(all);
    mp_http_stop(s);
}

MP_TEST(http_client_errors) {
    mp_err e = {{0}};
    mp_http_server *s = mp_http_start("127.0.0.1", 0, echo, NULL, &e);
    if (!s) {
        MP_FAIL_("%s", e.msg);
        return;
    }
    uint16_t port = mp_http_port(s);
    mp_http_stop(s);
    int st = 0;
    char *resp = NULL;
    size_t rl = 0;
    mp_err e2 = {{0}};
    uint64_t t0 = mp_now_ns();
    MP_CHECK(mp_http_request("127.0.0.1", port, "GET", "/", NULL, 0, 300, &st, &resp, &rl, &e2) == -1);
    MP_CHECK(e2.msg[0] != 0);
    MP_CHECK(mp_now_ns() - t0 < 2000000000ull);

    // A server that accepts the connection (the system's queue) and never responds.
    sock_t ls = socket(AF_INET, SOCK_STREAM, 0);
    struct sockaddr_in a;
    memset(&a, 0, sizeof a);
    a.sin_family = AF_INET;
    inet_pton(AF_INET, "127.0.0.1", &a.sin_addr);
    bind(ls, (struct sockaddr *)&a, sizeof a);
    listen(ls, 4);
    socklen_t al = sizeof a;
    getsockname(ls, (struct sockaddr *)&a, &al);
    mp_err e3 = {{0}};
    t0 = mp_now_ns();
    MP_CHECK(mp_http_request("127.0.0.1", ntohs(a.sin_port), "GET", "/", NULL, 0, 300, &st, &resp, &rl, &e3) == -1);
    MP_CHECK(e3.msg[0] != 0);
    MP_CHECK(mp_now_ns() - t0 < 2000000000ull);
    close_sock(ls);
}

MP_TEST(url_decode) {
    char out[64];
    MP_CHECK(mp_url_decode("%2Fmnt%2Fusb0%2Fa+b.p5z", out, sizeof out) == 0);
    MP_CHECK_STR(out, "/mnt/usb0/a b.p5z");
    MP_CHECK(mp_url_decode("%zz", out, sizeof out) == -1);
    MP_CHECK(mp_url_decode("%2", out, sizeof out) == -1);
    MP_CHECK(mp_url_decode("abcdef", out, 4) == -1);
    MP_CHECK(mp_http_query_get("a=1&p5z=%2Fx%2Fy.p5z&b=", "p5z", out, sizeof out) == 0);
    MP_CHECK_STR(out, "/x/y.p5z");
    MP_CHECK(mp_http_query_get("a=1", "p5z", out, sizeof out) == -1);
}

int main(void) {
    MP_RUN(http_roundtrip);
    MP_RUN(http_limits);
    MP_RUN(http_client_errors);
    MP_RUN(url_decode);
    MP_TEST_MAIN_END();
}
