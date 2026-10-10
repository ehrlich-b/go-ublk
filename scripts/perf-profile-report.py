#!/usr/bin/env python3
"""Normalize server perf counters by fio completions and summarize flag sets."""

import csv
import json
import math
from pathlib import Path
import statistics
import sys


EVENTS = ("raw_syscalls:sys_enter", "context-switches", "cpu-migrations")


def read_counters(path):
    counters = {}
    with Path(path).open() as source:
        for fields in csv.reader(source, delimiter=";"):
            if len(fields) < 3 or fields[0].startswith("#"):
                continue
            event = fields[2].strip()
            if event not in EVENTS:
                continue
            try:
                value = float(fields[0].strip().replace(",", ""))
            except ValueError as error:
                raise ValueError(f"{path}: {event} is unavailable: {fields[0]}") from error
            if not math.isfinite(value) or value < 0:
                raise ValueError(f"{path}: invalid count for {event}: {value}")
            counters[event] = counters.get(event, 0) + value
    missing = set(EVENTS) - counters.keys()
    if missing:
        raise ValueError(f"{path}: missing counters: {', '.join(sorted(missing))}")
    return [counters[event] for event in EVENTS]


def report_run(round_number, index, flags, workload, fio_path, perf_path):
    with Path(fio_path).open() as source:
        jobs = json.load(source)["jobs"]
    if len(jobs) != 1 or jobs[0].get("error", 0) != 0:
        raise ValueError(f"{fio_path}: expected one successful group-reported fio job")
    read = jobs[0]["read"]
    ios = int(read["total_ios"])
    if ios <= 0:
        raise ValueError(f"{fio_path}: no completed reads")
    percentiles = read["clat_ns"]["percentile"]
    latencies = [float(percentiles[key]) / 1000 for key in ("50.000000", "99.000000")]
    iops = float(read["iops"])
    if any(not math.isfinite(value) or value < 0 for value in [iops, *latencies]):
        raise ValueError(f"{fio_path}: invalid IOPS or latency")
    counters = read_counters(perf_path)
    fields = [round_number, index, flags, workload, f"{iops:.2f}"]
    fields += [f"{value:.3f}" for value in latencies]
    fields += [str(ios)] + [f"{value:.0f}" for value in counters]
    fields += [f"{value / ios:.6f}" for value in counters]
    print("\t".join(fields))


def summary(path):
    groups = {}
    with Path(path).open() as source:
        for row in csv.DictReader(source, delimiter="\t"):
            key = (int(row["set"]), row["flags"], row["workload"])
            groups.setdefault(key, []).append(row)
    print("\nPer flag set: median IOPS/p50/p99; counters divided by total completed I/Os")
    print("set\tflags\tworkload\trounds\tiops\tp50_us\tp99_us\tsyscalls/IO\tcontext-switches/IO\tmigrations/IO")
    for (index, flags, workload), rows in sorted(groups.items()):
        ios = sum(int(row["ios"]) for row in rows)
        medians = [statistics.median(float(row[key]) for row in rows)
                   for key in ("iops", "p50_us", "p99_us")]
        ratios = [sum(float(row[key]) for row in rows) / ios
                  for key in ("syscalls", "context_switches", "migrations")]
        fields = [str(index), flags, workload, str(len(rows))]
        fields += [f"{value:.3f}" for value in medians]
        fields += [f"{value:.6f}" for value in ratios]
        print("\t".join(fields))


def main():
    if len(sys.argv) == 3 and sys.argv[1] == "--summary":
        summary(sys.argv[2])
    elif len(sys.argv) == 7:
        report_run(*sys.argv[1:])
    else:
        raise ValueError("usage: perf-profile-report.py <round> <set> <flags> <workload> "
                         "<fio.json> <perf.csv> | --summary <results.tsv>")


if __name__ == "__main__":
    try:
        main()
    except (OSError, ValueError, KeyError, TypeError, json.JSONDecodeError) as error:
        print(f"perf-profile-report: {error}", file=sys.stderr)
        sys.exit(1)
