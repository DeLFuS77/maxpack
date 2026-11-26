// SPDX-License-Identifier: GPL-3.0-or-later
// The stall of a send (mp_send_stall_look): what this PC slept is never counted against the console.
#include <stdint.h>
#include <stdio.h>
#include <string.h>

#include "mp_send.h"
#include "mp_test.h"

#define S 1000000000ull
#define STALL (300 * S)

// Nothing moves for the stall, awake: it is stalled at the stall, not before.
MP_TEST(stall_awake) {
    mp_send_stall st;
    memset(&st, 0, sizeof st);
    st.moved = 10 * S;
    for (uint64_t t = 10; t < 310; t++) MP_CHECK(mp_send_stall_look(&st, t * S, t * S, STALL) == MP_STALL_NOT);
    MP_CHECK(mp_send_stall_look(&st, 310 * S, 310 * S, STALL) == MP_STALL_NOTHING_MOVED);
}

// The PC sleeps for less than the stall (the clock that counts sleep runs on, the awake one does not): that time is
// not the console's, and the stall starts again on waking. Stalled only once a whole stall passed awake, and then it
// says the PC slept meanwhile.
MP_TEST(stall_after_a_sleep) {
    mp_send_stall st;
    memset(&st, 0, sizeof st);
    st.moved = 10 * S;
    MP_CHECK(mp_send_stall_look(&st, 10 * S, 10 * S, STALL) == MP_STALL_NOT);
    MP_CHECK(mp_send_stall_look(&st, 200 * S, 200 * S, STALL) == MP_STALL_NOT);
    // 250 s asleep, then a few seconds awake: not stalled (it would have been, at 300 s since it moved)
    MP_CHECK(mp_send_stall_look(&st, 450 * S, 201 * S, STALL) == MP_STALL_NOT);
    MP_CHECK(mp_send_stall_look(&st, 460 * S, 211 * S, STALL) == MP_STALL_NOT);
    MP_CHECK(st.slept >= 249 * S);
    MP_CHECK(mp_send_stall_look(&st, 749 * S, 500 * S, STALL) == MP_STALL_NOT);
    MP_CHECK(mp_send_stall_look(&st, 751 * S, 502 * S, STALL) == MP_STALL_AFTER_SLEEP);
}

// One gap of at least the stall on both clocks between two looks (this program was stopped, or a clock that does
// not tell a sleep apart): it is said as a sleep of this PC, as before.
MP_TEST(stall_one_long_gap) {
    mp_send_stall st;
    memset(&st, 0, sizeof st);
    st.moved = 10 * S;
    MP_CHECK(mp_send_stall_look(&st, 10 * S, 10 * S, STALL) == MP_STALL_NOT);
    MP_CHECK(mp_send_stall_look(&st, 320 * S, 320 * S, STALL) == MP_STALL_GAP);
}

// Something moving starts it again, and forgets a sleep before it.
MP_TEST(stall_moved) {
    mp_send_stall st;
    memset(&st, 0, sizeof st);
    st.moved = 10 * S;
    mp_send_stall_look(&st, 10 * S, 10 * S, STALL);
    mp_send_stall_look(&st, 400 * S, 20 * S, STALL);   // a sleep
    mp_send_stall_moved(&st, 410 * S);
    MP_CHECK(st.slept == 0);
    MP_CHECK(mp_send_stall_look(&st, 709 * S, 329 * S, STALL) == MP_STALL_NOT);
    MP_CHECK(mp_send_stall_look(&st, 711 * S, 331 * S, STALL) == MP_STALL_NOTHING_MOVED);
}

int main(void) {
    MP_RUN(stall_awake);
    MP_RUN(stall_after_a_sleep);
    MP_RUN(stall_one_long_gap);
    MP_RUN(stall_moved);
    MP_TEST_MAIN_END();
}
