// bench launches an editor on a file and reports when its window appears,
// when it goes idle, the CPU it used and its memory footprint.
// Usage: bench <timeout-seconds> <executable> [args...]
import CoreGraphics
import Darwin
import Foundation

func now() -> Double {
    var t = timespec()
    clock_gettime(CLOCK_MONOTONIC, &t)
    return Double(t.tv_sec) + Double(t.tv_nsec) / 1e9
}

struct Usage { var cpu: Double; var footprint: UInt64 }

func usage(_ pid: pid_t) -> Usage? {
    var ri = rusage_info_v4()
    let r = withUnsafeMutablePointer(to: &ri) {
        $0.withMemoryRebound(to: rusage_info_t?.self, capacity: 1) {
            proc_pid_rusage(pid, RUSAGE_INFO_V4, $0)
        }
    }
    if r != 0 { return nil }
    var tb = mach_timebase_info()
    mach_timebase_info(&tb)
    let ns = Double(ri.ri_user_time + ri.ri_system_time) * Double(tb.numer) / Double(tb.denom)
    return Usage(cpu: ns / 1e9, footprint: ri.ri_phys_footprint)
}

// The process and all its descendants.
func tree(_ pid: pid_t) -> [pid_t] {
    var out = [pid]
    var buf = [pid_t](repeating: 0, count: 256)
    let n = proc_listchildpids(pid, &buf, Int32(buf.count * MemoryLayout<pid_t>.size))
    if n > 0 {
        for c in buf.prefix(Int(n)) where c > 0 { out += tree(c) }
    }
    return out
}

func total(_ pid: pid_t) -> Usage {
    var u = Usage(cpu: 0, footprint: 0)
    for p in tree(pid) {
        if let x = usage(p) { u.cpu += x.cpu; u.footprint += x.footprint }
    }
    return u
}

func hasWindow(_ pid: pid_t) -> Bool {
    guard let list = CGWindowListCopyWindowInfo([.optionOnScreenOnly], kCGNullWindowID) as? [[String: Any]] else { return false }
    for w in list {
        guard (w[kCGWindowOwnerPID as String] as? Int32) == pid,
              (w[kCGWindowLayer as String] as? Int) == 0,
              let b = w[kCGWindowBounds as String] as? [String: Double],
              (b["Width"] ?? 0) > 200, (b["Height"] ?? 0) > 200 else { continue }
        return true
    }
    return false
}

let args = CommandLine.arguments
let timeout = Double(args[1])!
let p = Process()
p.executableURL = URL(fileURLWithPath: args[2])
p.arguments = Array(args.dropFirst(3))
p.standardOutput = FileHandle.nullDevice
p.standardError = FileHandle.nullDevice
let t0 = now()
try! p.run()
let pid = p.processIdentifier

var windowAt = -1.0
var peak: UInt64 = 0
var lastBusy = now()
var lastCPU = 0.0
var lastCheck = now()
var idleAt = -1.0
while now() - t0 < timeout {
    usleep(10_000)
    let t = now()
    if windowAt < 0 && hasWindow(pid) { windowAt = t - t0 }
    let u = total(pid)
    peak = max(peak, u.footprint)
    // Every 100 ms: busy if it used more than 10% of a core.
    if t - lastCheck >= 0.1 {
        if u.cpu - lastCPU > 0.1 * (t - lastCheck) { lastBusy = t }
        lastCPU = u.cpu
        lastCheck = t
    }
    if windowAt >= 0 && t - lastBusy > 1.0 { idleAt = lastBusy - t0; break }
}
let u = total(pid)
let mb = { (b: UInt64) in String(format: "%.0f", Double(b) / 1048576) }
print(String(format: "window=%.3f idle=%.3f cpu=%.3f", windowAt, idleAt, u.cpu),
      "footprint=\(mb(u.footprint))MB peak=\(mb(peak))MB procs=\(tree(pid).count)")
for c in tree(pid).reversed() { kill(c, SIGKILL) }
p.waitUntilExit()
