import { open } from "fs/promises";

import sharp from "sharp";

import {
    type OmeTiffBaseMetadata,
    type OmeTiffMetadata,
    type OmeTiffPlaneCoordinates,
    parseOmeXmlMetadata,
    planeIndexForCoordinates,
} from "@/lib/ome-tiff";

const omeXmlPrefixBytes = 4 * 1024 * 1024;

type PlaneRenderOptions = OmeTiffPlaneCoordinates & {
    height: number;
    width: number;
};

type SharpStats = {
    channels: Array<{
        max: number;
        mean: number;
        min: number;
        stdev: number;
    }>;
};

function extractOmeXml(text: string): string | undefined {
    const start = text.indexOf("<OME");

    if (start < 0) {
        return undefined;
    }

    const end = text.indexOf("</OME>", start);

    if (end < 0) {
        return undefined;
    }

    return text.slice(start, end + "</OME>".length);
}

async function readOmeXmlPrefix(path: string): Promise<string | undefined> {
    const handle = await open(path, "r");

    try {
        const buffer = Buffer.alloc(omeXmlPrefixBytes);
        const { bytesRead } = await handle.read(buffer, 0, buffer.length, 0);

        return extractOmeXml(buffer.subarray(0, bytesRead).toString("utf8"));
    } finally {
        await handle.close();
    }
}

function baseMetadataFromSharp(metadata: sharp.Metadata): OmeTiffBaseMetadata {
    return {
        channels: metadata.channels,
        depth: metadata.depth,
        format: metadata.format,
        height: metadata.height,
        pages: metadata.pages,
        width: metadata.width,
    };
}

export async function getOmeTiffMetadata(
    path: string,
): Promise<OmeTiffMetadata> {
    const metadata = await sharp(path, { limitInputPixels: false }).metadata();
    const omeXml = await readOmeXmlPrefix(path).catch(() => undefined);

    return parseOmeXmlMetadata(omeXml, baseMetadataFromSharp(metadata));
}

function contrastStretch(stats: SharpStats, metadata: OmeTiffMetadata) {
    const channel = stats.channels[0];

    if (!channel) {
        return undefined;
    }

    const lower = channel.min;
    const upper = Math.min(
        channel.max,
        channel.mean + Math.max(1, channel.stdev) * 4,
    );

    if (!Number.isFinite(lower) || !Number.isFinite(upper) || upper <= lower) {
        return undefined;
    }

    const outputMaximum =
        metadata.depth === "ushort" || metadata.pixelType === "uint16"
            ? 65535
            : 255;
    const multiplier = outputMaximum / (upper - lower);

    return {
        multiplier,
        offset: -lower * multiplier,
    };
}

export async function renderOmeTiffPlane(
    path: string,
    metadata: OmeTiffMetadata,
    options: PlaneRenderOptions,
): Promise<Buffer> {
    const page = planeIndexForCoordinates(metadata, options);
    const sharpOptions = {
        limitInputPixels: false,
        page,
    };
    const stats = (await sharp(path, sharpOptions)
        .stats()
        .catch(() => null)) as SharpStats | null;
    const stretch = stats ? contrastStretch(stats, metadata) : undefined;
    let pipeline = sharp(path, sharpOptions);

    if (stretch) {
        pipeline = pipeline.linear(stretch.multiplier, stretch.offset);
    }

    return pipeline
        .resize({
            fit: "inside",
            height: options.height,
            kernel: sharp.kernel.lanczos3,
            width: options.width,
            withoutEnlargement: true,
        })
        .webp({ quality: 82 })
        .toBuffer();
}
