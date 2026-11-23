export async function fileToBase64(file: File): Promise<{ name: string; mime: string; data: string }> {
  const buffer = await file.arrayBuffer();
  const bytes = new Uint8Array(buffer);
  let binary = "";
  const chunk = 0x8000;
  for (let i = 0; i < bytes.length; i += chunk) {
    binary += String.fromCharCode(...bytes.subarray(i, i + chunk));
  }
  return {
    name: file.name || `pasted-${Date.now()}`,
    mime: file.type || "application/octet-stream",
    data: btoa(binary),
  };
}
