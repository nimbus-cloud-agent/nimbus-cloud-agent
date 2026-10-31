import { useState } from "react";
import { Button, Modal } from "@heroui/react";
import { X, Download } from "lucide-react";
import type { MessageImage } from "./api";

export default function ImageGallery({
  images,
  onRemove,
}: {
  images: MessageImage[];
  onRemove?: (id: string) => void;
}) {
  const [preview, setPreview] = useState<MessageImage | null>(null);
  if (!images.length) return null;
  return (
    <>
      <div className={"image-gallery" + (onRemove ? " image-drafts" : "")}>
        {images.map((image) => (
          <div className="image-tile" key={image.id}>
            <button
              type="button"
              className="image-open"
              onClick={() => setPreview(image)}
              aria-label={"预览图片：" + image.name}
            >
              <img src={image.url} alt={image.name} loading="lazy" />
              <span title={image.name}>{image.name}</span>
            </button>
            {onRemove && (
              <Button
                isIconOnly
                size="sm"
                variant="secondary"
                className="image-remove"
                aria-label={"移除图片：" + image.name}
                onPress={() => onRemove(image.id)}
              >
                <X size={13} />
              </Button>
            )}
          </div>
        ))}
      </div>
      <Modal
        isOpen={!!preview}
        onOpenChange={(open) => {
          if (!open) setPreview(null);
        }}
      >
        <Modal.Backdrop>
          <Modal.Container>
            <Modal.Dialog className="image-preview-dialog">
              <Modal.CloseTrigger />
              <Modal.Header>
                <Modal.Heading>{preview?.name}</Modal.Heading>
              </Modal.Header>
              <Modal.Body>
                {preview && (
                  <img
                    className="image-preview"
                    src={preview.url}
                    alt={preview.name}
                  />
                )}
              </Modal.Body>
              <Modal.Footer>
                {preview && (
                  <a
                    className="image-download"
                    href={preview.url}
                    download={preview.name}
                  >
                    <Download size={16} />
                    下载图片
                  </a>
                )}
              </Modal.Footer>
            </Modal.Dialog>
          </Modal.Container>
        </Modal.Backdrop>
      </Modal>
    </>
  );
}
