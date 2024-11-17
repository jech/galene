'use strict';

let imageSegmenter;

async function loadImageSegmenter() {
    let module = await import('/third-party/tasks-vision/vision_bundle.mjs');

    let vision = await module.FilesetResolver.forVisionTasks(
        "/third-party/tasks-vision/wasm"
    );

    imageSegmenter =
        await module.ImageSegmenter.createFromOptions(vision, {
            baseOptions: {
                modelAssetPath: '/third-party/tasks-vision/selfie_segmenter.tflite',
            },
            outputCategoryMask: true,
            outputConfidenceMasks: false,
            runningMode: 'VIDEO',
        });
}

loadImageSegmenter();

onmessage = e => {
    if(!imageSegmenter) {
        // not ready yet
        postMessage(null);
        return;
    }

    try {
        let bitmap = e.data.bitmap;
        if(!(bitmap instanceof ImageBitmap))
            throw new Error('Bad type for worker data');
        let width = bitmap.width;
        let height = bitmap.height;
        imageSegmenter.segmentForVideo(
            bitmap, e.data.timestamp,
            result => {
                /** @type{Uint8Array} */
                let mask = result.categoryMask.getAsUint8Array();
                let id = new ImageData(width, height);
                for(let i = 0; i < mask.length; i++)
                    id.data[4 * i + 3] = mask[i];
                createImageBitmap(id).then(ib => {
                    postMessage({
                        bitmap: bitmap,
                        mask: ib,
                    }, [bitmap, ib]);
                });
            },
        );
    } catch(e) {
        postMessage(e);
    }
};
