#!/usr/bin/env python3
"""Tests for the backup alarm's decision (AOC-030).

⭐ WHY THESE EXIST. The alarm is the only thing standing between "the cron stopped firing" and
"we find out when we need the backup". A check nobody has seen fail is a check nobody has
tested, so every way it is supposed to go red is pinned here — including the two that look
like passes: an EMPTY listing, and a check that could not look at all.

Run: python3 -m unittest discover -s scripts -p 'test_*.py'
"""
import datetime as dt
import unittest

from check_backup_freshness import Obj, judge

NOW = dt.datetime(2026, 9, 22, 12, 0, 0, tzinfo=dt.timezone.utc)


def obj(hours_ago, size=5_000_000, key=None):
    stamp = NOW - dt.timedelta(hours=hours_ago)
    return Obj(key or f"prod/aoc-prod-{stamp:%Y%m%dT%H%M%SZ}.dump", size, stamp)


class TestJudge(unittest.TestCase):
    def judge(self, objects, max_age_hours=48, min_bytes=1024):
        return judge(objects, NOW, max_age_hours, min_bytes, prefix="prod/")

    # ---- the green path ----------------------------------------------------
    def test_a_recent_plausible_dump_passes(self):
        ok, lines = self.judge([obj(3)])
        self.assertTrue(ok, lines)

    def test_it_judges_the_NEWEST_not_the_first_listed(self):
        # Order must not matter: an old object in the bucket is normal (retention window),
        # and a listing is not guaranteed to arrive sorted the way we expect.
        ok, _ = self.judge([obj(200), obj(3), obj(100)])
        self.assertTrue(ok)

    def test_an_old_object_alongside_a_fresh_one_is_fine(self):
        ok, _ = self.judge([obj(47), obj(1)])
        self.assertTrue(ok)

    # ---- stale -------------------------------------------------------------
    def test_stale_fails(self):
        ok, lines = self.judge([obj(49)])
        self.assertFalse(ok)
        self.assertTrue(any("STALE" in l for l in lines), lines)

    def test_the_boundary_is_not_off_by_one(self):
        self.assertTrue(self.judge([obj(47.9)])[0])
        self.assertFalse(self.judge([obj(48.1)])[0])

    def test_every_object_stale_fails_even_when_there_are_many(self):
        ok, _ = self.judge([obj(72), obj(96), obj(120)])
        self.assertFalse(ok)

    # ---- the failures that look like passes --------------------------------
    def test_an_EMPTY_listing_fails(self):
        # ⚠️ The one that matters most. "Nothing to check" must never read as "nothing wrong" —
        # it is what a wrong prefix, an empty bucket and a job that never ran all look like.
        ok, lines = self.judge([])
        self.assertFalse(ok)
        self.assertTrue(any("NO OBJECTS" in l for l in lines), lines)

    def test_a_zero_byte_object_fails(self):
        ok, lines = self.judge([obj(1, size=0)])
        self.assertFalse(ok)
        self.assertTrue(any("TOO SMALL" in l for l in lines), lines)

    def test_a_small_error_message_uploaded_as_a_dump_fails(self):
        ok, lines = self.judge([obj(1, size=57)])
        self.assertFalse(ok)
        self.assertTrue(any("TOO SMALL" in l for l in lines), lines)

    def test_a_future_timestamp_fails(self):
        # A clock that is wrong is how a stale backup reads as fresh.
        ok, lines = self.judge([obj(-10)])
        self.assertFalse(ok)
        self.assertTrue(any("IMPOSSIBLE" in l for l in lines), lines)

    def test_small_clock_skew_is_tolerated(self):
        # Seconds of skew between R2 and the runner are normal and must not page anyone.
        ok, _ = self.judge([obj(-1 / 60.0)])
        self.assertTrue(ok)

    # ---- both wrong at once ------------------------------------------------
    def test_stale_AND_tiny_reports_both(self):
        ok, lines = self.judge([obj(100, size=0)])
        self.assertFalse(ok)
        self.assertTrue(any("STALE" in l for l in lines), lines)
        self.assertTrue(any("TOO SMALL" in l for l in lines), lines)

    # ---- it never raises ---------------------------------------------------
    def test_judge_never_raises_on_odd_input(self):
        for objects in ([], [obj(0, size=0)], [obj(1e6)], [obj(-1e6)]):
            ok, lines = self.judge(objects)
            self.assertIsInstance(ok, bool)
            self.assertTrue(lines)


if __name__ == "__main__":
    unittest.main()
