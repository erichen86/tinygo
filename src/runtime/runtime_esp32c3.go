// +build esp32c3

package runtime

import (
	"device/esp"
	"device/riscv"
	"machine"
	"unsafe"
)

type timeUnit int64

func putchar(c byte) {
	machine.Serial.WriteByte(c)
}

func postinit() {}

// This is the function called on startup after the flash (IROM/DROM) is
// initialized and the stack pointer has been set.
//export main
func main() {
	// This initialization configures the following things:
	// * It disables all watchdog timers. They might be useful at some point in
	//   the future, but will need integration into the scheduler. For now,
	//   they're all disabled.
	// * It sets the CPU frequency to 160MHz, which is the maximum speed allowed
	//   for this CPU. Lower frequencies might be possible in the future, but
	//   running fast and sleeping quickly is often also a good strategy to save
	//   power.
	// TODO: protect certain memory regions, especially the area below the stack
	// to protect against stack overflows. See
	// esp_cpu_configure_region_protection in ESP-IDF.

	// Disable Timer 0 watchdog.
	esp.TIMG0.WDTCONFIG0_REG.Set(0)

	// Disable RTC watchdog.
	esp.RTC_CNTL.RTC_WDTWPROTECT.Set(0x50D83AA1)
	esp.RTC_CNTL.RTC_WDTCONFIG0.Set(0)

	// Disable super watchdog.
	esp.RTC_CNTL.RTC_SWD_WPROTECT.Set(0x8F1D312A)
	esp.RTC_CNTL.RTC_SWD_CONF.Set(esp.RTC_CNTL_RTC_SWD_CONF_SWD_DISABLE)

	// Change CPU frequency from 20MHz to 80MHz, by switching from the XTAL to
	// the PLL clock source (see table "CPU Clock Frequency" in the reference
	// manual).
	esp.SYSTEM.SYSCLK_CONF.Set(1 << esp.SYSTEM_SYSCLK_CONF_SOC_CLK_SEL_Pos)

	// Change CPU frequency from 80MHz to 160MHz by setting SYSTEM_CPUPERIOD_SEL
	// to 1 (see table "CPU Clock Frequency" in the reference manual).
	// Note: we might not want to set SYSTEM_CPU_WAIT_MODE_FORCE_ON to save
	// power. It is set here to keep the default on reset.
	esp.SYSTEM.CPU_PER_CONF_REG.Set(esp.SYSTEM_CPU_PER_CONF_REG_CPU_WAIT_MODE_FORCE_ON | esp.SYSTEM_CPU_PER_CONF_REG_PLL_FREQ_SEL | 1<<esp.SYSTEM_CPU_PER_CONF_REG_CPUPERIOD_SEL_Pos)

	// Initialize .bss: zero-initialized global variables.
	// The .data section has already been loaded by the ROM bootloader.
	ptr := unsafe.Pointer(&_sbss)
	for ptr != unsafe.Pointer(&_ebss) {
		*(*uint32)(ptr) = 0
		ptr = unsafe.Pointer(uintptr(ptr) + 4)
	}

	// Configure timer 0 in timer group 0, for timekeeping.
	//   EN:       Enable the timer.
	//   INCREASE: Count up every tick (as opposed to counting down).
	//   DIVIDER:  16-bit prescaler, set to 2 for dividing the APB clock by two
	//             (40MHz).
	esp.TIMG0.T0CONFIG_REG.Set(esp.TIMG_T0CONFIG_REG_T0_EN | esp.TIMG_T0CONFIG_REG_T0_INCREASE | 2<<esp.TIMG_T0CONFIG_REG_T0_DIVIDER_Pos)

	// Set the timer counter value to 0.
	esp.TIMG0.T0LOADLO_REG.Set(0)
	esp.TIMG0.T0LOADHI_REG.Set(0)
	esp.TIMG0.T0LOAD_REG.Set(0) // value doesn't matter.

	// Initialize the heap, call main.main, etc.
	run()

	// Fallback: if main ever returns, hang the CPU.
	abort()
}

func ticks() timeUnit {
	// First, update the LO and HI register pair by writing any value to the
	// register. This allows reading the pair atomically.
	esp.TIMG0.T0UPDATE_REG.Set(0)
	// Then read the two 32-bit parts of the timer.
	return timeUnit(uint64(esp.TIMG0.T0LO_REG.Get()) | uint64(esp.TIMG0.T0HI_REG.Get())<<32)
}

func nanosecondsToTicks(ns int64) timeUnit {
	// Calculate the number of ticks from the number of nanoseconds. At a 80MHz
	// APB clock, that's 25 nanoseconds per tick with a timer prescaler of 2:
	// 25 = 1e9 / (80MHz / 2)
	return timeUnit(ns / 25)
}

func ticksToNanoseconds(ticks timeUnit) int64 {
	// See nanosecondsToTicks.
	return int64(ticks) * 25
}

func sleepTicks(d timeUnit) {
	sleepUntil := ticks() + d
	for ticks() < sleepUntil {
		// TODO: suspend the CPU to not burn power here unnecessarily.
	}
}

func abort() {
	// lock up forever
	for {
		riscv.Asm("wfi")
	}
}
