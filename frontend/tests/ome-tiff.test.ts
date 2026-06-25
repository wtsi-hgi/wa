import { describe, expect, it } from "vitest";

import {
    buildOmeTiffMetadataUrl,
    buildOmeTiffPlaneUrl,
    parseOmeXmlMetadata,
    planeIndexForCoordinates,
} from "@/lib/ome-tiff";

const omeXml = `<?xml version="1.0" encoding="UTF-8"?>
<OME xmlns="http://www.openmicroscopy.org/Schemas/OME/2016-06">
  <Image ID="Image:0">
    <Pixels ID="Pixels:0:0" DimensionOrder="XYZCT" Type="uint16" SizeX="2160" SizeY="2160" SizeZ="16" SizeC="4" SizeT="1">
      <Channel ID="Channel:0:0" Name="ch1 - PhenoVue Fluor 488" SamplesPerPixel="1"/>
      <Channel ID="Channel:0:1" Name="ch2 - PhenoVue 641 Mito Stain" SamplesPerPixel="1"/>
      <Channel ID="Channel:0:2" Name="ch3 - PhenoVue Hoechst 33342" SamplesPerPixel="1"/>
      <Channel ID="Channel:0:3" Name="ch4 - PhenoVue Fluor 568" SamplesPerPixel="1"/>
      <TiffData IFD="0" PlaneCount="64"/>
    </Pixels>
  </Image>
</OME>`;

describe("OME-TIFF metadata helpers", () => {
    it("parses OME XML dimensions and channel names from multichannel TIFF metadata", () => {
        const metadata = parseOmeXmlMetadata(omeXml, {
            channels: 1,
            depth: "ushort",
            format: "tiff",
            height: 2160,
            pages: 64,
            width: 2160,
        });

        expect(metadata).toMatchObject({
            channelCount: 4,
            depth: "ushort",
            dimensionOrder: "XYZCT",
            format: "tiff",
            hasOmeMetadata: true,
            height: 2160,
            pageCount: 64,
            pixelType: "uint16",
            sizeT: 1,
            sizeX: 2160,
            sizeY: 2160,
            sizeZ: 16,
            width: 2160,
        });
        expect(metadata.channels.map((channel) => channel.name)).toEqual([
            "ch1 - PhenoVue Fluor 488",
            "ch2 - PhenoVue 641 Mito Stain",
            "ch3 - PhenoVue Hoechst 33342",
            "ch4 - PhenoVue Fluor 568",
        ]);
    });

    it("maps OME channel and Z coordinates to the page order used by XYZCT stacks", () => {
        const metadata = parseOmeXmlMetadata(omeXml, {
            channels: 1,
            depth: "ushort",
            format: "tiff",
            height: 2160,
            pages: 64,
            width: 2160,
        });

        expect(
            planeIndexForCoordinates(metadata, {
                channel: 2,
                t: 0,
                z: 3,
            }),
        ).toBe(35);
        expect(
            planeIndexForCoordinates(metadata, {
                channel: 3,
                t: 0,
                z: 15,
            }),
        ).toBe(63);
    });

    it("builds metadata and derived plane URLs without asking the browser for the full TIFF", () => {
        const proxyUrl =
            "/api/file?id=result-1&path=%2Ftmp%2Fresults%2Fstack.ome.tiff";

        expect(buildOmeTiffMetadataUrl(proxyUrl)).toBe(
            "/api/file?id=result-1&path=%2Ftmp%2Fresults%2Fstack.ome.tiff&ome=metadata",
        );
        expect(
            buildOmeTiffPlaneUrl(proxyUrl, {
                channel: 2,
                height: 420,
                t: 0,
                width: 672,
                z: 3,
            }),
        ).toBe(
            "/api/file?id=result-1&path=%2Ftmp%2Fresults%2Fstack.ome.tiff&ome=plane&channel=2&z=3&t=0&w=672&h=420",
        );
    });
});
