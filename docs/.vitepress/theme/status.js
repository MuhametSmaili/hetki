import { onBeforeUnmount, onMounted, ref } from "vue";

const spinnerFrames = ["⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"];
export const statusGlyphs = {
  blocked: "?",
  idle: "○",
  success: "✓",
  error: "×",
  unknown: "·",
};
export const statusLabels = {
  working: "Working",
  blocked: "Needs input",
  idle: "Idle",
  success: "Reported success",
  error: "Reported error",
  unknown: "Unknown",
};

// Each mounted caller owns its observer, listeners, and animation timer.
export function useStatusGlyph(target) {
  const frame = ref(spinnerFrames[0]);
  let cleanup = () => {};

  onMounted(() => {
    const motion = matchMedia("(prefers-reduced-motion: reduce)");
    let inView = false;
    let disposed = false;
    let timer;
    let index = 0;

    function update() {
      if (disposed) return;

      const animate = inView && !document.hidden && !motion.matches;
      if (animate && timer === undefined) {
        timer = setInterval(() => {
          index = (index + 1) % spinnerFrames.length;
          frame.value = spinnerFrames[index];
        }, 100);
      } else if (!animate) {
        clearInterval(timer);
        timer = undefined;
      }
    }

    const observer = new IntersectionObserver(([entry]) => {
      inView = entry.isIntersecting;
      update();
    });
    observer.observe(target.value);
    motion.addEventListener("change", update);
    document.addEventListener("visibilitychange", update);

    cleanup = () => {
      disposed = true;
      clearInterval(timer);
      observer.disconnect();
      motion.removeEventListener("change", update);
      document.removeEventListener("visibilitychange", update);
    };
  });

  onBeforeUnmount(() => cleanup());
  return (status) =>
    status === "working" ? frame.value : (statusGlyphs[status] ?? "");
}
