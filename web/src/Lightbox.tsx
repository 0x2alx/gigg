// Lightbox: one image full size in a dialog (Esc, ✕ or a click beside it closes). The Attachments
// section and images in Markdown (descriptions, comments) both open it.
import type { ReactNode } from "react";
import { Modal } from "./ui";

export function Lightbox({ src, title, alt, foot, onClose }: { src: string; title: string; alt: string; foot?: ReactNode; onClose: () => void }) {
  return (
    <Modal title={title} onClose={onClose} xl>
      <div className="att-lightbox" onClick={(e) => e.target === e.currentTarget && onClose()}>
        <img src={src} alt={alt} />
      </div>
      {foot}
    </Modal>
  );
}
