'use strict';

let imageSegmenter;

async function loadImageSegmenter(model) {
    let module = await import('/third-party/tasks-vision/vision_bundle.mjs');
    let vision = await module.FilesetResolver.forVisionTasks(
        "/third-party/tasks-vision/wasm"
    );

    return await module.ImageSegmenter.createFromOptions(vision, {
            baseOptions: {
                modelAssetPath: model,
            },
            outputCategoryMask: true,
            outputConfidenceMasks: false,
            runningMode: 'VIDEO',
        });
}

onmessage = async e => {
    let data = e.data;
    if(imageSegmenter == null) {
        try {
            imageSegmenter = await loadImageSegmenter(data.model);
            if(imageSegmenter == null)
                throw new Error("loadImageSegmenter returned null");
        } catch(e) {
            postMessage(e);
            return;
        }
        postMessage(null);
        return;
    }

    let bitmap = e.data.bitmap;
    if(!(bitmap instanceof ImageBitmap)) {
        postMessage(new Error('Bad type for worker data'));
        return;
    }

    try {
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
                result.close();
                createImageBitmap(id).then(ib => {
                    postMessage({
                        bitmap: bitmap,
                        mask: ib,
                    }, [bitmap, ib]);
                });
            },
        );
    } catch(e) {
        bitmap.close();
        postMessage(e);
    }
};
