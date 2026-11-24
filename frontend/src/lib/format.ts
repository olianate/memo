export function formatTime(value: string): string {
  const d = new Date(value);
  if (Number.isNaN(d.getTime())) return "";
  return `${String(d.getHours()).padStart(2, "0")}:${String(d.getMinutes()).padStart(2, "0")}`;
}

export function formatDate(value: string): string {
  const d = new Date(value);
  if (Number.isNaN(d.getTime())) return "";
  return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, "0")}-${String(d.getDate()).padStart(2, "0")} ${formatTime(value)}`;
}

export function dayLabel(day: string): { title: string; sub: string } {
  const d = new Date(`${day}T12:00:00`);
  if (Number.isNaN(d.getTime())) return { title: day, sub: "" };
  const today = new Date();
  const startOf = (x: Date) => new Date(x.getFullYear(), x.getMonth(), x.getDate()).getTime();
  const diff = Math.round((startOf(today) - startOf(d)) / 86_400_000);
  const week = ["日", "一", "二", "三", "四", "五", "六"][d.getDay()];
  if (diff === 0) return { title: "今天", sub: `${d.getMonth() + 1}月${d.getDate()}日 · 周${week}` };
  if (diff === 1) return { title: "昨天", sub: `${d.getMonth() + 1}月${d.getDate()}日 · 周${week}` };
  if (d.getFullYear() === today.getFullYear()) {
    return { title: `${d.getMonth() + 1}月${d.getDate()}日`, sub: `周${week}` };
  }
  return { title: `${d.getFullYear()}年${d.getMonth() + 1}月${d.getDate()}日`, sub: `周${week}` };
}

export function formatMonth(month: string): string {
  const [year, m] = month.split("-");
  return `${year}年${Number(m)}月`;
}

export function countWords(text: string): number {
  let count = 0;
  let inWord = false;
  for (const ch of text) {
    if (/[\u3040-\u30ff\u3400-\u9fff\uac00-\ud7af]/.test(ch)) {
      count += 1;
      inWord = false;
    } else if (/[\p{L}\p{N}]/u.test(ch)) {
      if (!inWord) count += 1;
      inWord = true;
    } else {
      inWord = false;
    }
  }
  return count;
}

export function humanBytes(bytes: number): string {
  if (!bytes) return "0 B";
  const units = ["B", "KB", "MB", "GB"];
  let value = bytes;
  let unit = 0;
  while (value >= 1024 && unit < units.length - 1) {
    value /= 1024;
    unit += 1;
  }
  return `${value >= 10 || unit === 0 ? Math.round(value) : value.toFixed(1)} ${units[unit]}`;
}

export function relativeTime(value?: string): string {
  if (!value) return "从未";
  const then = new Date(value).getTime();
  if (Number.isNaN(then)) return "从未";
  const seconds = Math.max(0, Math.round((Date.now() - then) / 1000));
  if (seconds < 45) return "刚刚";
  if (seconds < 3600) return `${Math.round(seconds / 60)} 分钟前`;
  if (seconds < 86_400) return `${Math.round(seconds / 3600)} 小时前`;
  return `${Math.round(seconds / 86_400)} 天前`;
}

export function currentMonth(): string {
  const d = new Date();
  return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, "0")}`;
}

export function startOfDayIso(date: string): string {
  return new Date(`${date}T00:00:00`).toISOString();
}

export function endOfDayIso(date: string): string {
  const d = new Date(`${date}T00:00:00`);
  d.setDate(d.getDate() + 1);
  return d.toISOString();
}
