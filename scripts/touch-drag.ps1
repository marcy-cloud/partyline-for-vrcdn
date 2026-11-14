# Drags with a real (injected) touch contact, for ui-smoke.mjs: finger down
# at screen pixel X,Y, moved by DX,DY in steps, and lifted; without DX and DY
# it is a tap. This is how a
# touch screen, and a VR controller pointer that acts as one, reaches the app.
#
# It only touches the window of process $AppId: that window is brought to the
# front first, and if something else is still under the point, nothing is
# touched and the script fails.
param([Parameter(Mandatory)][int]$AppId, [Parameter(Mandatory)][int]$X, [Parameter(Mandatory)][int]$Y, [int]$DX = 0, [int]$DY = 0)

Add-Type @"
using System;
using System.Runtime.InteropServices;
public static class Touch {
    [StructLayout(LayoutKind.Sequential)] public struct POINT { public int x, y; }
    [StructLayout(LayoutKind.Sequential)] public struct RECT { public int left, top, right, bottom; }
    [StructLayout(LayoutKind.Sequential)] public struct POINTER_INFO {
        public uint pointerType, pointerId, frameId, pointerFlags;
        public IntPtr sourceDevice, hwndTarget;
        public POINT ptPixelLocation, ptHimetricLocation, ptPixelLocationRaw, ptHimetricLocationRaw;
        public uint dwTime, historyCount;
        public int inputData;
        public uint dwKeyStates;
        public ulong PerformanceCount;
        public int ButtonChangeType;
    }
    [StructLayout(LayoutKind.Sequential)] public struct POINTER_TOUCH_INFO {
        public POINTER_INFO pointerInfo;
        public uint touchFlags, touchMask;
        public RECT rcContact, rcContactRaw;
        public uint orientation, pressure;
    }
    [DllImport("user32.dll", SetLastError = true)] public static extern bool InitializeTouchInjection(uint maxCount, uint mode);
    [DllImport("user32.dll", SetLastError = true)] public static extern bool InjectTouchInput(uint count, [In] POINTER_TOUCH_INFO[] contacts);
    [DllImport("user32.dll")] public static extern bool SetProcessDPIAware();
    [DllImport("user32.dll")] public static extern IntPtr WindowFromPoint(POINT p);
    [DllImport("user32.dll")] public static extern IntPtr GetAncestor(IntPtr hwnd, uint flags);
    [DllImport("user32.dll")] public static extern uint GetWindowThreadProcessId(IntPtr hwnd, out uint pid);
    [DllImport("user32.dll")] public static extern bool SetForegroundWindow(IntPtr hwnd);

    // The process owning the top-level window under a screen point.
    public static uint OwnerAt(int x, int y) {
        uint pid;
        GetWindowThreadProcessId(GetAncestor(WindowFromPoint(new POINT { x = x, y = y }), 2), out pid);
        return pid;
    }

    public static void Send(int x, int y, uint flags) {
        var c = new POINTER_TOUCH_INFO();
        c.pointerInfo.pointerType = 2; // PT_TOUCH
        c.pointerInfo.pointerFlags = flags;
        c.pointerInfo.ptPixelLocation.x = x;
        c.pointerInfo.ptPixelLocation.y = y;
        c.touchMask = 1 | 2 | 4; // contact area, orientation, pressure
        c.rcContact.left = x - 2; c.rcContact.right = x + 2;
        c.rcContact.top = y - 2; c.rcContact.bottom = y + 2;
        c.orientation = 90; c.pressure = 32000;
        if (!InjectTouchInput(1, new[] { c }))
            throw new System.ComponentModel.Win32Exception(Marshal.GetLastWin32Error());
    }
}
"@

$inRange = 0x2; $inContact = 0x4; $down = 0x10000; $update = 0x20000; $up = 0x40000
[void][Touch]::SetProcessDPIAware()
$main = (Get-Process -Id $AppId).MainWindowHandle
for ($i = 0; $i -lt 8 -and [Touch]::OwnerAt($X, $Y) -ne $AppId; $i++) {
    (New-Object -ComObject WScript.Shell).SendKeys('%')  # lets a background process take the foreground
    [void][Touch]::SetForegroundWindow($main)
    Start-Sleep -Milliseconds 250
}
if ([Touch]::OwnerAt($X, $Y) -ne $AppId) { Write-Error 'another window is in the way; nothing was touched'; exit 1 }
if (-not [Touch]::InitializeTouchInjection(1, 1)) { throw 'touch injection is not available' }

[Touch]::Send($X, $Y, $down -bor $inRange -bor $inContact)
# With nowhere to go this is a tap, which must be short: a finger held still
# is a press-and-hold, Windows's touch for a right click.
$steps = if ($DX -or $DY) { 20 } else { 0 }
for ($i = 1; $i -le $steps; $i++) {
    Start-Sleep -Milliseconds 25
    [Touch]::Send($X + [int]($DX * $i / $steps), $Y + [int]($DY * $i / $steps), $update -bor $inRange -bor $inContact)
}
Start-Sleep -Milliseconds $(if ($steps) { 150 } else { 60 })
[Touch]::Send($X + $DX, $Y + $DY, $up)
