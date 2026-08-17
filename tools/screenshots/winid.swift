import CoreGraphics
import Foundation

// Prints the CGWindowID of the first on-screen window whose owner name contains the given
// substring (default "welock-screens"). Used to `screencapture -l <id>` a window WITHOUT
// raising it or stealing focus — so it doesn't matter what you're doing in parallel.
let target = CommandLine.arguments.count > 1 ? CommandLine.arguments[1] : "welock-screens"
let opts: CGWindowListOption = [.optionOnScreenOnly, .excludeDesktopElements]
guard let list = CGWindowListCopyWindowInfo(opts, kCGNullWindowID) as? [[String: Any]] else {
    exit(1)
}
for wnd in list {
    let owner = (wnd[kCGWindowOwnerName as String] as? String) ?? ""
    let layer = (wnd[kCGWindowLayer as String] as? Int) ?? 0
    if layer == 0 && owner.lowercased().contains(target.lowercased()),
       let num = wnd[kCGWindowNumber as String] as? Int {
        print(num)
        exit(0)
    }
}
exit(2)
