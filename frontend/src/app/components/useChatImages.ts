'use client';

import { useCallback, useRef, useState, type ClipboardEvent } from 'react';

export function useChatImages() {
  const imageInputRef = useRef<HTMLInputElement>(null);
  const [attachedImages, setAttachedImages] = useState<
    Array<{
      name: string;
      mimeType: string;
      data: string;
    }>
  >([]);
  const handleImageSelection = useCallback(async (files: FileList | File[] | null) => {
    if (!files) return;
    const selected = Array.from(files).filter((file) => file.type.startsWith('image/'));
    const encoded = await Promise.all(
      selected.map(
        (file) =>
          new Promise<{
            name: string;
            mimeType: string;
            data: string;
          }>((resolve, reject) => {
            const reader = new FileReader();
            reader.onerror = () => reject(reader.error ?? new Error(`Failed to read ${file.name}`));
            reader.onload = () => {
              const result = String(reader.result ?? '');
              resolve({
                name: file.name,
                mimeType: file.type,
                data: result.split(',', 2)[1] ?? '',
              });
            };
            reader.readAsDataURL(file);
          })
      )
    );
    setAttachedImages((prev) => [...prev, ...encoded].slice(0, 4));
    if (imageInputRef.current) imageInputRef.current.value = '';
  }, []);

  const handleImagePaste = useCallback(
    (event: ClipboardEvent<HTMLTextAreaElement>) => {
      const images = Array.from(event.clipboardData.items)
        .filter((item) => item.kind === 'file' && item.type.startsWith('image/'))
        .map((item) => item.getAsFile())
        .filter((file): file is File => file !== null);
      if (images.length === 0) return;

      event.preventDefault();
      void handleImageSelection(images);
    },
    [handleImageSelection]
  );

  return { imageInputRef, attachedImages, setAttachedImages, handleImageSelection, handleImagePaste };
}
