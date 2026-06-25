export type OmeTiffChannel = {
    index: number;
    name: string;
};

export type OmeTiffBaseMetadata = {
    channels?: number;
    depth?: string;
    format?: string;
    height?: number;
    pages?: number;
    width?: number;
};

export type OmeTiffMetadata = {
    channelCount: number;
    channels: OmeTiffChannel[];
    depth?: string;
    dimensionOrder: string;
    format: string;
    hasOmeMetadata: boolean;
    height: number;
    pageCount: number;
    physicalSizeX?: number;
    physicalSizeY?: number;
    physicalSizeZ?: number;
    pixelType?: string;
    sizeT: number;
    sizeX: number;
    sizeY: number;
    sizeZ: number;
    width: number;
};

export type OmeTiffPlaneCoordinates = {
    channel: number;
    t: number;
    z: number;
};

export type OmeTiffPlaneUrlOptions = OmeTiffPlaneCoordinates & {
    height: number;
    width: number;
};

type XmlAttributes = Record<string, string>;

const defaultDimensionOrder = "XYCZT";
const defaultPlaneDimensionOrder: Array<"C" | "T" | "Z"> = ["C", "Z", "T"];
const planeDimensions = new Set(["Z", "C", "T"]);

function decodeXmlAttribute(value: string): string {
    return value
        .replaceAll("&quot;", '"')
        .replaceAll("&apos;", "'")
        .replaceAll("&gt;", ">")
        .replaceAll("&lt;", "<")
        .replaceAll("&amp;", "&");
}

function parseXmlAttributes(tag: string): XmlAttributes {
    const attributes: XmlAttributes = {};
    const attributePattern = /([\w:.-]+)\s*=\s*(["'])(.*?)\2/g;

    for (const match of tag.matchAll(attributePattern)) {
        const key = match[1];
        const value = match[3];

        if (key && value !== undefined) {
            attributes[key] = decodeXmlAttribute(value);
        }
    }

    return attributes;
}

function positiveIntegerAttribute(
    attributes: XmlAttributes,
    name: string,
    fallback: number,
): number {
    const parsed = Number(attributes[name]);

    if (!Number.isInteger(parsed) || parsed < 1) {
        return fallback;
    }

    return parsed;
}

function optionalNumberAttribute(
    attributes: XmlAttributes,
    name: string,
): number | undefined {
    const parsed = Number(attributes[name]);

    return Number.isFinite(parsed) ? parsed : undefined;
}

function clampIndex(value: number, size: number): number {
    if (size <= 0) {
        return 0;
    }

    if (!Number.isFinite(value)) {
        return 0;
    }

    return Math.min(size - 1, Math.max(0, Math.round(value)));
}

function fallbackChannelName(index: number, hasOmeMetadata: boolean): string {
    return hasOmeMetadata ? `Channel ${index + 1}` : `Plane ${index + 1}`;
}

function buildChannels(
    count: number,
    names: string[],
    hasOmeMetadata: boolean,
): OmeTiffChannel[] {
    return Array.from({ length: count }, (_, index) => ({
        index,
        name:
            names[index]?.trim() || fallbackChannelName(index, hasOmeMetadata),
    }));
}

export function fallbackOmeTiffMetadata(
    base: OmeTiffBaseMetadata,
): OmeTiffMetadata {
    const pageCount = Math.max(1, Math.round(base.pages ?? 1));
    const width = Math.max(0, Math.round(base.width ?? 0));
    const height = Math.max(0, Math.round(base.height ?? 0));
    const channelCount = pageCount > 1 ? pageCount : 1;

    return {
        channelCount,
        channels: buildChannels(channelCount, [], false),
        depth: base.depth,
        dimensionOrder: defaultDimensionOrder,
        format: base.format ?? "tiff",
        hasOmeMetadata: false,
        height,
        pageCount,
        sizeT: 1,
        sizeX: width,
        sizeY: height,
        sizeZ: 1,
        width,
    };
}

export function parseOmeXmlMetadata(
    xml: string | null | undefined,
    base: OmeTiffBaseMetadata,
): OmeTiffMetadata {
    const fallback = fallbackOmeTiffMetadata(base);

    if (!xml) {
        return fallback;
    }

    const pixelsTag = xml.match(/<Pixels\b[^>]*>/)?.[0];

    if (!pixelsTag) {
        return fallback;
    }

    const attributes = parseXmlAttributes(pixelsTag);
    const sizeX = positiveIntegerAttribute(attributes, "SizeX", fallback.width);
    const sizeY = positiveIntegerAttribute(
        attributes,
        "SizeY",
        fallback.height,
    );
    const sizeZ = positiveIntegerAttribute(attributes, "SizeZ", 1);
    const channelCount = positiveIntegerAttribute(attributes, "SizeC", 1);
    const sizeT = positiveIntegerAttribute(attributes, "SizeT", 1);
    const pageCount = Math.max(
        1,
        Math.round(base.pages ?? sizeZ * channelCount * sizeT),
    );
    const channelNames = [...xml.matchAll(/<Channel\b[^>]*>/g)].map(
        (match) => parseXmlAttributes(match[0])["Name"] ?? "",
    );

    return {
        channelCount,
        channels: buildChannels(channelCount, channelNames, true),
        depth: base.depth,
        dimensionOrder:
            attributes["DimensionOrder"]?.trim().toUpperCase() ||
            defaultDimensionOrder,
        format: base.format ?? "tiff",
        hasOmeMetadata: true,
        height: sizeY,
        pageCount,
        physicalSizeX: optionalNumberAttribute(attributes, "PhysicalSizeX"),
        physicalSizeY: optionalNumberAttribute(attributes, "PhysicalSizeY"),
        physicalSizeZ: optionalNumberAttribute(attributes, "PhysicalSizeZ"),
        pixelType: attributes["Type"],
        sizeT,
        sizeX,
        sizeY,
        sizeZ,
        width: sizeX,
    };
}

export function planeIndexForCoordinates(
    metadata: OmeTiffMetadata,
    coordinates: OmeTiffPlaneCoordinates,
): number {
    if (!metadata.hasOmeMetadata) {
        return clampIndex(coordinates.channel, metadata.pageCount);
    }

    const sizes: Record<"C" | "T" | "Z", number> = {
        C: metadata.channelCount,
        T: metadata.sizeT,
        Z: metadata.sizeZ,
    };
    const coordinateValues: Record<"C" | "T" | "Z", number> = {
        C: coordinates.channel,
        T: coordinates.t,
        Z: coordinates.z,
    };
    const dimensions = [...metadata.dimensionOrder.toUpperCase()].filter(
        (dimension): dimension is "C" | "T" | "Z" =>
            planeDimensions.has(dimension),
    );
    const orderedDimensions =
        dimensions.length > 0 ? dimensions : defaultPlaneDimensionOrder;
    let multiplier = 1;
    let page = 0;

    for (const dimension of orderedDimensions) {
        page +=
            clampIndex(coordinateValues[dimension], sizes[dimension]) *
            multiplier;
        multiplier *= sizes[dimension];
    }

    return clampIndex(page, metadata.pageCount);
}

export function buildOmeTiffMetadataUrl(proxyUrl: string): string {
    const queryIndex = proxyUrl.indexOf("?");
    const path = queryIndex >= 0 ? proxyUrl.slice(0, queryIndex) : proxyUrl;
    const query =
        queryIndex >= 0
            ? new URLSearchParams(proxyUrl.slice(queryIndex + 1))
            : new URLSearchParams();

    query.set("ome", "metadata");

    return `${path}?${query.toString()}`;
}

export function buildOmeTiffPlaneUrl(
    proxyUrl: string,
    options: OmeTiffPlaneUrlOptions,
): string {
    const queryIndex = proxyUrl.indexOf("?");
    const path = queryIndex >= 0 ? proxyUrl.slice(0, queryIndex) : proxyUrl;
    const query =
        queryIndex >= 0
            ? new URLSearchParams(proxyUrl.slice(queryIndex + 1))
            : new URLSearchParams();

    query.set("ome", "plane");
    query.set(
        "channel",
        String(clampIndex(options.channel, Number.MAX_SAFE_INTEGER)),
    );
    query.set("z", String(clampIndex(options.z, Number.MAX_SAFE_INTEGER)));
    query.set("t", String(clampIndex(options.t, Number.MAX_SAFE_INTEGER)));
    query.set("w", String(Math.max(1, Math.round(options.width))));
    query.set("h", String(Math.max(1, Math.round(options.height))));

    return `${path}?${query.toString()}`;
}

export function isTiffPreviewPath(path: string): boolean {
    const normalized = path.split("?")[0]?.toLowerCase() ?? path.toLowerCase();

    return normalized.endsWith(".tif") || normalized.endsWith(".tiff");
}
