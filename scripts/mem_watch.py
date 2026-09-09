#!/usr/bin/env python3
"""Lance une commande en sous-process et échantillonne son RSS + le cgroup.

Usage : python3 mem_watch.py <cmd...>
Print [t=XXs] rss_mb=XXXX peak=XXXX avail=XXXX chaque 2 s. Si le process
meurt (OOM kill), imprime la dernière valeur RSS connue.
"""
import subprocess
import sys
import time


def read_avail_mb() -> float:
    try:
        with open("/proc/meminfo") as f:
            for line in f:
                if line.startswith("MemAvailable"):
                    return int(line.split()[1]) / 1024
    except OSError:
        pass
    return -1.0


def rss_mb(pid: int) -> float:
    try:
        with open(f"/proc/{pid}/statm") as f:
            return int(f.read().split()[1]) * 4096 / (1024 * 1024)
    except OSError:
        return -1.0


def main() -> int:
    proc = subprocess.Popen(sys.argv[1:])
    peak = 0.0
    last = 0.0
    t0 = time.time()
    while proc.poll() is None:
        r = rss_mb(proc.pid)
        if r > 0:
            last = r
            peak = max(peak, r)
        print(f"[t={time.time()-t0:5.0f}s] rss={last:7.0f}MB peak={peak:7.0f}MB avail={read_avail_mb():7.0f}MB", flush=True)
        time.sleep(2)
    print(f"FIN rc={proc.returncode} last_rss={last:.0f}MB peak={peak:.0f}MB", flush=True)
    return proc.returncode or 0


if __name__ == "__main__":
    sys.exit(main())
