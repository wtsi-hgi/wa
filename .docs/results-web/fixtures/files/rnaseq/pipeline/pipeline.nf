nextflow.enable.dsl = 2

workflow {
  Channel
    .from("sample-sheet.csv")
    .set { samples }

  samples.view { "processing ${it}" }
}
