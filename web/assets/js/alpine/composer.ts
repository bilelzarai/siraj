/* The message composer (MS-02): the emoji picker, the files waiting to go, and the
   voice note the design records in place. The server does the sending; this only
   collects what the one form posts. */
export function composer() {
  return {
    emoji: false,
    recording: false,
    seconds: 0,
    files: [] as { name: string; size: string }[],
    media: null as MediaRecorder | null,
    chunks: [] as Blob[],
    tick: 0,
    get clock() {
      const s = this.seconds;
      return `${Math.floor(s / 60)}:${String(s % 60).padStart(2, "0")}`;
    },
    /** add puts an emoji where the caret is, which is what the picker is for. */
    add(this: any, mark: string) {
      const box = this.$refs.body as HTMLInputElement;
      const at = box.selectionStart ?? box.value.length;
      box.value = box.value.slice(0, at) + mark + box.value.slice(at);
      box.focus();
      box.setSelectionRange(at + mark.length, at + mark.length);
      this.emoji = false;
    },
    /** picked shows the chip the design draws under the composer, and turns a file
        the server would refuse away here, where the player can still pick another. */
    picked(this: any) {
      const input = this.$refs.file as HTMLInputElement;
      const chosen = Array.from(input.files ?? []);
      const max = Number(this.$el.dataset.maxBytes) || 0;
      const tooBig = max > 0 ? chosen.find((f) => f.size > max) : undefined;
      if (tooBig) {
        input.value = "";
        this.files = [];
        // The sentence is translated by the server; only the file's own name and size
        // are filled in here, because only here are they known.
        window.sjToast?.(
          (this.$el.dataset.tooBig ?? "")
            .replace("{name}", tooBig.name)
            .replace("{size}", readable(tooBig.size)),
        );
        return;
      }
      this.files = chosen.map((f) => ({ name: f.name, size: readable(f.size) }));
    },
    drop(this: any) {
      (this.$refs.file as HTMLInputElement).value = "";
      this.files = [];
    },
    async startRecording(this: any) {
      try {
        const stream = await navigator.mediaDevices.getUserMedia({ audio: true });
        this.chunks = [];
        this.media = new MediaRecorder(stream);
        this.media.ondataavailable = (e: BlobEvent) => this.chunks.push(e.data);
        this.media.onstop = () => {
          stream.getTracks().forEach((t) => t.stop());
          if (!this.keep) return;
          const blob = new Blob(this.chunks, { type: "audio/webm" });
          const file = new File([blob], "voice-note.webm", { type: "audio/webm" });
          const box = new DataTransfer();
          box.items.add(file);
          (this.$refs.file as HTMLInputElement).files = box.files;
          this.picked();
        };
        this.keep = true;
        this.media.start();
        this.recording = true;
        this.seconds = 0;
        this.tick = window.setInterval(() => (this.seconds += 1), 1000);
      } catch {
        window.sjToast?.(this.$el.dataset.noMic ?? "");
      }
    },
    stopRecording(this: any) {
      this.keep = true;
      this.finish();
    },
    cancelRecording(this: any) {
      this.keep = false;
      this.finish();
    },
    finish(this: any) {
      window.clearInterval(this.tick);
      this.media?.stop();
      this.recording = false;
    },
  };
}

/** The same ceiling the server keeps (messages.MaxUpload). */

/** readable is the size a chip prints: the same rounding the server uses. */
function readable(bytes: number) {
  if (bytes >= 1024 * 1024) return `${(bytes / 1024 / 1024).toFixed(1)} MB`;
  if (bytes >= 1024) return `${Math.round(bytes / 1024)} KB`;
  return `${bytes} B`;
}

/* A voice note plays in its own bubble, and the bars fill as it goes (MS-02). */
export function voice(duration: string) {
  return {
    playing: false,
    duration,
    toggle(this: any) {
      const audio = this.$refs.audio as HTMLAudioElement | undefined;
      if (!audio) {
        // No file behind it (a seeded note): still show the state the design draws.
        this.playing = !this.playing;
        return;
      }
      if (this.playing) {
        audio.pause();
        this.playing = false;
        return;
      }
      audio.currentTime = 0;
      audio.play().then(() => {
        this.playing = true;
        this.$el.style.setProperty("--d", `${audio.duration || 6}s`);
      }).catch(() => {});
      audio.onended = () => (this.playing = false);
    },
  };
}
